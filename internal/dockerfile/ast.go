package dockerfile

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/docker/cli/opts"
	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"github.com/pkg/errors"

	"github.com/tilt-dev/tilt/internal/container"
)

type AST struct {
	directives []*parser.Directive
	result     *parser.Result
}

func ParseAST(df Dockerfile) (AST, error) {
	result, err := parser.Parse(newReader(df))
	if err != nil {
		return AST{}, errors.Wrap(err, "dockerfile.ParseAST")
	}

	dParser := &parser.DirectiveParser{}
	directives, err := dParser.ParseAll([]byte(df))
	if err != nil {
		return AST{}, errors.Wrap(err, "dockerfile.ParseAST")
	}

	return AST{
		directives: directives,
		result:     result,
	}, nil
}

func (a AST) extractBaseNameInFromCommand(node *parser.Node, shlex *shell.Lex, metaArgs []instructions.ArgCommand) string {
	if node.Next == nil {
		return ""
	}

	inst, err := instructions.ParseInstruction(node)
	if err != nil {
		return node.Next.Value // if there's a parsing error, fallback to the first arg
	}

	fromInst, ok := inst.(*instructions.Stage)
	if !ok || fromInst.BaseName == "" {
		return ""
	}

	// The base image name may have ARG expansions in it. Do the default
	// substitution.
	argsMap := fakeArgsMap(shlex, metaArgs)
	baseName, err := shlex.ProcessWordWithMap(fromInst.BaseName, argsMap)
	if err != nil {
		// If anything fails, just use the hard-coded BaseName.
		return fromInst.BaseName
	}
	return baseName

}

// Find all images referenced in this dockerfile and call the visitor function.
// If the visitor function returns a new image, substitute that image into the dockerfile.
func (a AST) traverseImageRefs(visitor func(node *parser.Node, ref reference.Named) reference.Named, dockerfileArgs []instructions.ArgCommand) error {
	metaArgs := append([]instructions.ArgCommand(nil), dockerfileArgs...)
	shlex := shell.NewLex(a.result.EscapeToken)

	return a.Traverse(func(node *parser.Node) error {
		switch strings.ToLower(node.Value) {
		case command.Arg:
			inst, err := instructions.ParseInstruction(node)
			if err != nil {
				return nil // ignore parsing error
			}

			argCmd, ok := inst.(*instructions.ArgCommand)
			if !ok {
				return nil
			}

			// args within the Dockerfile are prepended because they provide defaults that are overridden by actual args
			metaArgs = append([]instructions.ArgCommand{*argCmd}, metaArgs...)

		case command.From:
			baseName := a.extractBaseNameInFromCommand(node, shlex, metaArgs)
			if baseName == "" {
				return nil // ignore parsing error
			}

			ref, err := container.ParseNamed(baseName)
			if err != nil {
				return nil // drop the error, we don't care about malformed images
			}
			newRef := visitor(node, ref)
			if newRef != nil {
				node.Next.Value = container.FamiliarString(newRef)
			}

		case command.Copy:
			if len(node.Flags) == 0 {
				return nil
			}

			inst, err := instructions.ParseInstruction(node)
			if err != nil {
				return nil // ignore parsing error
			}

			copyCmd, ok := inst.(*instructions.CopyCommand)
			if !ok {
				return nil
			}

			ref, err := container.ParseNamed(copyCmd.From)
			if err != nil {
				return nil // drop the error, we don't care about malformed images
			}

			newRef := visitor(node, ref)
			if newRef != nil {
				for i, flag := range node.Flags {
					if strings.HasPrefix(flag, "--from=") {
						node.Flags[i] = fmt.Sprintf("--from=%s", container.FamiliarString(newRef))
					}
				}
			}
		}

		return nil
	})
}

func (a AST) InjectImageDigest(selector container.RefSelector, ref reference.NamedTagged, buildArgs []string) (bool, error) {
	modified := false
	err := a.traverseImageRefs(func(node *parser.Node, toReplace reference.Named) reference.Named {
		if selector.Matches(toReplace) {
			modified = true
			return ref
		}
		return nil
	}, argInstructions(buildArgs))
	return modified, err
}

// Post-order traversal of the Dockerfile AST.
// Halts immediately on error.
func (a AST) Traverse(visit func(*parser.Node) error) error {
	return a.traverseNode(a.result.AST, visit)
}

func (a AST) traverseNode(node *parser.Node, visit func(*parser.Node) error) error {
	for _, c := range node.Children {
		err := a.traverseNode(c, visit)
		if err != nil {
			return err
		}
	}
	return visit(node)
}

func (a AST) Print() (Dockerfile, error) {
	buf := bytes.NewBuffer(nil)
	currentLine := 1

	directiveFmt := "# %s = %s\n"
	for _, v := range a.directives {
		_, err := fmt.Fprintf(buf, directiveFmt, v.Name, v.Value)
		if err != nil {
			return "", err
		}
		currentLine++
	}

	for _, node := range a.result.AST.Children {
		for currentLine < node.StartLine {
			_, err := buf.Write([]byte("\n"))
			if err != nil {
				return "", err
			}
			currentLine++
		}

		lineCount, err := a.printNode(node, buf)
		if err != nil {
			return "", err
		}

		currentLine = node.StartLine + lineCount
	}
	return Dockerfile(buf.String()), nil
}

// Loosely adapted from
// https://github.com/jessfraz/dockfmt/blob/master/format.go
// Returns the number of lines printed.
func (a AST) printNode(node *parser.Node, writer io.Writer) (int, error) {
	var v string

	// format per directive
	switch strings.ToLower(node.Value) {
	// all the commands that use parseMaybeJSON
	// https://github.com/moby/buildkit/blob/2ec7d53b00f24624cda0adfbdceed982623a93b3/frontend/dockerfile/parser/parser.go#L152
	case command.Cmd, command.Entrypoint, command.Run, command.Shell:
		v = fmtCmd(node)
	case command.Label:
		v = fmtLabel(node)
	default:
		v = fmtDefault(node)
	}

	_, err := fmt.Fprintln(writer, v)
	if err != nil {
		return 0, err
	}
	return strings.Count(v, "\n") + 1, nil
}

func getCmd(n *parser.Node) []string {
	if n == nil {
		return nil
	}

	cmd := []string{strings.ToUpper(n.Value)}
	if len(n.Flags) > 0 {
		cmd = append(cmd, n.Flags...)
	}

	return append(cmd, getCmdArgs(n)...)
}

func getCmdArgs(n *parser.Node) []string {
	if n == nil {
		return nil
	}

	cmd := []string{}
	for node := n.Next; node != nil; node = node.Next {
		cmd = append(cmd, node.Value)
		if len(node.Flags) > 0 {
			cmd = append(cmd, node.Flags...)
		}
	}

	return cmd
}

func appendHeredocs(node *parser.Node, cmdLine string) string {
	if len(node.Heredocs) == 0 {
		return cmdLine
	}
	lines := []string{cmdLine}
	for _, h := range node.Heredocs {
		lines = append(lines, fmt.Sprintf("\n%s%s", h.Content, h.Name))
	}
	return strings.Join(lines, "")
}

func fmtCmd(node *parser.Node) string {
	if node.Attributes["json"] {
		cmd := []string{strings.ToUpper(node.Value)}
		if len(node.Flags) > 0 {
			cmd = append(cmd, node.Flags...)
		}

		encoded := []string{}
		for _, c := range getCmdArgs(node) {
			encoded = append(encoded, fmt.Sprintf("%q", c))
		}
		return appendHeredocs(node, fmt.Sprintf("%s [%s]", strings.Join(cmd, " "), strings.Join(encoded, ", ")))
	}

	cmd := getCmd(node)
	return appendHeredocs(node, strings.Join(cmd, " "))
}

func fmtDefault(node *parser.Node) string {
	cmd := getCmd(node)
	return appendHeredocs(node, strings.Join(cmd, " "))
}

func fmtLabel(node *parser.Node) string {
	cmd := getCmd(node)
	assignments := []string{cmd[0]}
	for i := 1; i < len(cmd); i += 2 {
		if i+1 < len(cmd) {
			assignments = append(assignments, fmt.Sprintf("%s=%s", cmd[i], cmd[i+1]))
		} else {
			assignments = append(assignments, cmd[i])
		}
	}
	return strings.Join(assignments, " ")
}

func newReader(df Dockerfile) io.Reader {
	return bytes.NewBufferString(string(df))
}

// Loosely adapted from the buildkit code for turning args into a map.
// Iterate through them and do substitutions in order.
func fakeArgsMap(shlex *shell.Lex, args []instructions.ArgCommand) map[string]string {
	m := make(map[string]string)
	for _, argCmd := range args {
		val := ""
		for _, a := range argCmd.Args {
			if a.Value != nil {
				val, _ = shlex.ProcessWordWithMap(*(a.Value), m)
			}
			m[a.Key] = val
		}
	}
	return m
}

// argInstructions converts a map of build arguments into a slice of ArgCommand structs.
//
// Since the map guarantees uniqueness, there is no defined order of the resulting slice.
func argInstructions(buildArgs []string) []instructions.ArgCommand {
	var out []instructions.ArgCommand
	for k, v := range opts.ConvertKVStringsToMapWithNil(buildArgs) {
		out = append(out, instructions.ArgCommand{Args: []instructions.KeyValuePairOptional{
			{
				Key:   k,
				Value: v,
			},
		}})
	}
	return out
}
