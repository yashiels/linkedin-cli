package api

import (
	"bytes"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

type leverFormState struct {
	activeProviderControl bool
	activeSubmitControl   bool
	submitProxy           bool
}

func hasActiveLeverApplicationForm(body []byte, pageURL string) bool {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return false
	}
	return findActiveLeverApplicationForm(document, pageURL)
}

func findActiveLeverApplicationForm(node *html.Node, pageURL string) bool {
	if node.Type == html.ElementNode && node.Data == "template" {
		return false
	}
	if node.Type == html.ElementNode && node.Data == "form" && isLeverApplicationForm(node, pageURL) {
		return leverFormHasActiveControl(node, pageURL)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if findActiveLeverApplicationForm(child, pageURL) {
			return true
		}
	}
	return false
}

func isLeverApplicationForm(form *html.Node, pageURL string) bool {
	formID, _ := htmlAttribute(form, "id")
	method, _ := htmlAttribute(form, "method")
	if formID != "application-form" || !strings.EqualFold(strings.TrimSpace(method), "post") {
		return false
	}
	action, _ := htmlAttribute(form, "action")
	return leverTargetMatches(pageURL, action)
}

func leverFormHasActiveControl(form *html.Node, pageURL string) bool {
	state := leverFormState{}
	visitLeverFormControls(form, form, pageURL, &state)
	return state.activeSubmitControl || state.activeProviderControl && state.submitProxy
}

func visitLeverFormControls(node, form *html.Node, pageURL string, state *leverFormState) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "template" {
			if child.Type == html.ElementNode && (child.Data == "button" || child.Data == "input") {
				updateLeverFormState(state, child, form, pageURL)
			}
			visitLeverFormControls(child, form, pageURL, state)
		}
	}
}

func updateLeverFormState(state *leverFormState, control, form *html.Node, pageURL string) {
	if !leverControlBelongsToForm(control, form) || leverControlDisabled(control, form) || !leverControlTargetMatches(control, form, pageURL) {
		return
	}
	controlType, _ := htmlAttribute(control, "type")
	controlType = strings.ToLower(strings.TrimSpace(controlType))
	if controlType == "" && control.Data == "button" {
		controlType = "submit"
	}
	hidden := leverControlHidden(control, controlType)
	if controlType == "submit" {
		if hidden {
			state.submitProxy = true
		} else {
			state.activeSubmitControl = true
		}
	}
	dataQA, _ := htmlAttribute(control, "data-qa")
	if !hidden && control.Data == "button" && controlType == "button" && dataQA == "btn-submit" {
		state.activeProviderControl = true
	}
}

func leverControlBelongsToForm(control, form *html.Node) bool {
	owner, present := htmlAttribute(control, "form")
	if !present {
		return true
	}
	formID, _ := htmlAttribute(form, "id")
	return owner == formID
}

func leverControlTargetMatches(control, form *html.Node, pageURL string) bool {
	method, _ := htmlAttribute(form, "method")
	if override, present := htmlAttribute(control, "formmethod"); present {
		method = override
	}
	if !strings.EqualFold(strings.TrimSpace(method), "post") {
		return false
	}
	action, _ := htmlAttribute(form, "action")
	if override, present := htmlAttribute(control, "formaction"); present {
		action = override
	}
	return leverTargetMatches(pageURL, action)
}

func leverTargetMatches(pageURL, action string) bool {
	if strings.TrimSpace(action) == "" {
		return true
	}
	page, err := url.Parse(pageURL)
	if err != nil {
		return false
	}
	target, err := page.Parse(action)
	if err != nil || applicationProvider(target.String(), "Lever") != "lever" {
		return false
	}
	return strings.EqualFold(target.Hostname(), page.Hostname()) && applicationIdentity(target, "lever") == applicationIdentity(page, "lever")
}

func leverControlDisabled(control, form *html.Node) bool {
	if htmlBooleanAttribute(control, "disabled") {
		return true
	}
	ariaDisabled, _ := htmlAttribute(control, "aria-disabled")
	if strings.EqualFold(strings.TrimSpace(ariaDisabled), "true") {
		return true
	}
	for parent := control.Parent; parent != nil && parent != form; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "fieldset" && htmlBooleanAttribute(parent, "disabled") {
			return true
		}
	}
	return false
}

func leverControlHidden(control *html.Node, controlType string) bool {
	if controlType == "hidden" {
		return true
	}
	for node := control; node != nil; node = node.Parent {
		if htmlNodeHidden(node) {
			return true
		}
	}
	return false
}

func htmlNodeHidden(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	if htmlBooleanAttribute(node, "hidden") {
		return true
	}
	ariaHidden, _ := htmlAttribute(node, "aria-hidden")
	if strings.EqualFold(strings.TrimSpace(ariaHidden), "true") {
		return true
	}
	className, _ := htmlAttribute(node, "class")
	for _, value := range strings.Fields(strings.ToLower(className)) {
		if value == "hidden" {
			return true
		}
	}
	style, _ := htmlAttribute(node, "style")
	style = strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, strings.ToLower(style))
	return strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden")
}

func htmlBooleanAttribute(node *html.Node, name string) bool {
	_, present := htmlAttribute(node, name)
	return present
}

func htmlAttribute(node *html.Node, name string) (string, bool) {
	for _, attribute := range node.Attr {
		if attribute.Namespace == "" && attribute.Key == name {
			return attribute.Val, true
		}
	}
	return "", false
}
