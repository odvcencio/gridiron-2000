package main

import (
	"fmt"
	"strconv"
	"strings"

	"gridiron-2000/internal/setupwizard"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/session"
)

type wizardFieldKind int

const (
	wizardFieldText wizardFieldKind = iota
	wizardFieldNumber
	wizardFieldTextarea
	wizardFieldRadio
)

// wizardOption is one radio choice.
type wizardOption struct {
	Value, Label string
}

// wizardField is one rendered form control. Every wizard step page is
// built from a list of these, so every step shares one rendering and one
// visual language instead of ten hand-built forms.
type wizardField struct {
	Name    string
	Label   string
	Kind    wizardFieldKind
	Value   string
	Rows    int
	Options []wizardOption
	Help    string
}

func renderWizardField(f wizardField) gosx.Node {
	var body []any
	body = append(body, gosx.Attrs(gosx.Attr("class", "wizard-field")))
	body = append(body, gosx.El("label", gosx.Attrs(gosx.Attr("for", "field-"+f.Name)), gosx.Text(f.Label)))
	switch f.Kind {
	case wizardFieldTextarea:
		rows := f.Rows
		if rows <= 0 {
			rows = 4
		}
		body = append(body, gosx.El("textarea", gosx.Attrs(
			gosx.Attr("id", "field-"+f.Name), gosx.Attr("name", f.Name), gosx.Attr("rows", strconv.Itoa(rows)),
		), gosx.Text(f.Value)))
	case wizardFieldRadio:
		for _, opt := range f.Options {
			optAttrs := gosx.Attrs(
				gosx.Attr("type", "radio"), gosx.Attr("name", f.Name), gosx.Attr("value", opt.Value),
				gosx.Attr("id", "field-"+f.Name+"-"+opt.Value),
			)
			if opt.Value == f.Value {
				optAttrs = append(optAttrs, gosx.BoolAttr("checked"))
			}
			body = append(body, gosx.El("label", gosx.Attrs(gosx.Attr("class", "wizard-radio-option"), gosx.Attr("for", "field-"+f.Name+"-"+opt.Value)),
				gosx.El("input", optAttrs),
				gosx.Text(" "+opt.Label),
			))
		}
	case wizardFieldNumber:
		body = append(body, gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "number"), gosx.Attr("id", "field-"+f.Name), gosx.Attr("name", f.Name), gosx.Attr("value", f.Value),
		)))
	default:
		body = append(body, gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "text"), gosx.Attr("id", "field-"+f.Name), gosx.Attr("name", f.Name), gosx.Attr("value", f.Value),
		)))
	}
	if f.Help != "" {
		body = append(body, gosx.El("p", gosx.Attrs(gosx.Attr("class", "wizard-field-help")), gosx.Text(f.Help)))
	}
	return gosx.El("div", body...)
}

// wizardStepNavNode renders the step header list design section 4.4
// requires: every step, its typed status (DONE/CURRENT/TODO/STALE —
// words, not color), and a link for any revisitable (DONE or STALE) step.
// status may be nil (an all-TODO map).
func wizardStepNavNode(status setupwizard.StatusMap, currentSlug string) gosx.Node {
	var items []any
	items = append(items, gosx.Attrs(gosx.Attr("class", "wizard-step-nav")))
	for i, step := range setupwizard.Steps {
		state := status.StatusFor(step.Slug, currentSlug)
		label := fmt.Sprintf("%d. %s — %s", i+1, step.Title, state)
		revisitable := state == setupwizard.StepDone || state == setupwizard.StepStale
		var entry gosx.Node
		if revisitable && state != setupwizard.StepCurrent {
			entry = gosx.El("a", gosx.Attrs(gosx.Attr("href", "/setup/"+step.Slug), gosx.Attr("data-gosx-link", true)), gosx.Text(label))
		} else {
			entry = gosx.Text(label)
		}
		itemClass := "wizard-step-nav__item wizard-step-nav__item--" + strings.ToLower(string(state))
		items = append(items, gosx.El("li", gosx.Attrs(gosx.Attr("class", itemClass)), entry))
	}
	return gosx.El("ol", items...)
}

// wizardStepPage renders one step's complete page: the step nav, title,
// any warnings from the last save, any validation error, the field list
// inside a POST form targeting /setup/<slug>, and a Back link. status may
// be nil.
func wizardStepPage(ctx *route.RouteContext, status setupwizard.StatusMap, slug, title string, fields []wizardField, formError string, warnings []string) gosx.Node {
	csrf := session.Token(ctx.Request)
	var formBody []any
	formBody = append(formBody,
		gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/setup/"+slug)),
		gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", csrf))),
	)
	for _, field := range fields {
		formBody = append(formBody, renderWizardField(field))
	}
	formBody = append(formBody, gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit"), gosx.Attr("class", "button button--primary")), gosx.Text("Save and continue")))

	var page []any
	page = append(page, gosx.Attrs(gosx.Attr("class", "page wizard-page"), gosx.Attr("id", "main-content")))
	page = append(page, wizardStepNavNode(status, slug))
	page = append(page, gosx.El("h1", nil, gosx.Text(title)))
	if formError != "" {
		page = append(page, gosx.El("p", gosx.Attrs(gosx.Attr("class", "flash-message")), gosx.Text(formError)))
	}
	for _, warning := range warnings {
		page = append(page, gosx.El("p", gosx.Attrs(gosx.Attr("class", "wizard-warning")), gosx.Text("Note: "+warning)))
	}
	if prev := setupwizard.PrevStepSlug(slug); prev != "" {
		page = append(page, gosx.El("a", gosx.Attrs(gosx.Attr("href", "/setup/"+prev), gosx.Attr("class", "button button--ghost"), gosx.Attr("data-gosx-link", true)), gosx.Text("← Back")))
	}
	page = append(page, ctx.Form(formBody...))
	return gosx.El("main", page...)
}

// linesFromTextarea splits a textarea submission into trimmed, non-empty
// lines.
func linesFromTextarea(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// atoiOr parses raw as an int, returning fallback on any parse failure —
// used only for pre-filling a numeric field's display value, never for a
// value that reaches LoadConfigBytes as anything other than the exact
// string the operator typed (validateConfig, not this helper, is the
// authority on range).
func atoiOr(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}
