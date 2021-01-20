import React, { useEffect } from "react"
import { Link } from "react-router-dom"
import styled from "styled-components"
import { ReactComponent as CloseSvg } from "./assets/svg/close.svg"
import { ReactComponent as LogoWordmarkSvg } from "./assets/svg/logo-wordmark-gray.svg"
import {
  AnimDuration,
  Color,
  ColorAlpha,
  ColorRGBA,
  Font,
  FontSize,
  SizeUnit,
} from "./style-helpers"
import { useTabNav } from "./TabNav"

type OverviewTabBarProps = {
  selectedTab: string
}

let OverviewTabBarRoot = styled.div`
  padding-top: ${SizeUnit(0.5)};
  display: flex;
  width: 100%;
  height: 60px;
  box-sizing: border-box;
  background-color: ${Color.grayDarkest};
  border-bottom: 1px solid ${Color.grayLight};
  align-items: stretch;
`

export let Tab = styled(Link)`
  cursor: pointer;
  display: flex;
  font-family: ${Font.sansSerif};
  font-size: ${FontSize.smallest};
  flex-grow: 0;
  color: ${Color.grayLightest};
  align-items: stretch;
  text-decoration: none;
  padding-left: ${SizeUnit(0.5)};
  box-sizing: border-box;
  // "Remove" the Tab's bottom border (i.e., OverviewTabBarRoot's border)
  margin-bottom: -1px;
  // Define borders now so it doesn't "jump" when selected
  border-top: 1px solid ${ColorRGBA(Color.black, 0)};
  border-left: 1px solid ${ColorRGBA(Color.black, 0)};
  border-right: 1px solid ${ColorRGBA(Color.black, 0)};

  transition: background-color ${AnimDuration.short} linear,
    color ${AnimDuration.default} linear,
    border-color ${AnimDuration.short} linear;

  &:focus {
    outline: 0;
    border-top-color: ${Color.gray};
  }

  &:hover {
    color: ${Color.white};
  }

  &.isSelected {
    color: ${Color.white};
    background-color: ${Color.grayDark};
    border-top-color: ${Color.grayLight};
    border-left-color: ${Color.grayLight};
    border-right-color: ${Color.grayLight};
    border-radius: 4px 4px 0px 0px;
  }
`

let TabName = styled.div`
  display: flex;
  align-items: center;
`

export let HomeTab = styled(Link)`
  border: none;
  padding-left: ${SizeUnit(1)};
  padding-right: ${SizeUnit(1)};
  background-color: transparent;
  display: flex;
  align-items: center;
  opacity: ${ColorAlpha.almostOpaque};
  transition: opacity ${AnimDuration.short} ease;

  &:hover {
    opacity: 1;
  }
`

let CloseButton = styled.button`
  cursor: pointer;
  background-color: transparent;
  border: 0 none;
  padding-left: ${SizeUnit(0.25)};
  padding-right: ${SizeUnit(0.25)};

  svg {
    fill: none;
  }

  ${Tab}:hover & svg,
  ${Tab}.isSelected & svg {
    fill: ${Color.grayLightest};
  }
`

export default function OverviewTabBar(props: OverviewTabBarProps) {
  let nav = useTabNav()
  let tabs = nav.tabs
  let selectedTab = props.selectedTab

  // There are two bits of state to determine the selected tab:
  //
  // 1) If the user navigates to a url, we need to pull the candidate tab name from the URL.
  // 2) Then we need to look at the Tilt state to see if that resource exists.
  //
  // If the resource exists, then we select that tab. We need propagate it back
  // up to the context provider. This creates weird data flow, but is probably
  // ok for this simple case.
  useEffect(() => {
    nav.ensureSelectedTab(selectedTab)
  }, [selectedTab, nav.selectedTab])

  let onClose = (e: any, name: string) => {
    e.stopPropagation()
    e.preventDefault()
    nav.closeTab(name)
  }

  let tabEls = tabs.map((name) => {
    let href = `/r/${name}/overview`
    let text = name
    let isSelectedTab = false
    if (selectedTab === name) {
      isSelectedTab = true
    }
    return (
      <Tab key={name} to={href} className={isSelectedTab ? "isSelected" : ""}>
        <TabName>{text}</TabName>
        <CloseButton onClick={(e) => onClose(e, name)}>
          <CloseSvg />
        </CloseButton>
      </Tab>
    )
  })
  tabEls.unshift(
    <HomeTab key="logo" to={"/overview"} className={selectedTab}>
      <LogoWordmarkSvg width="57px" />
    </HomeTab>
  )
  return <OverviewTabBarRoot>{tabEls}</OverviewTabBarRoot>
}
