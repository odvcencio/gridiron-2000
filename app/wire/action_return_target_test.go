package wire

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"gridiron-2000/internal/actionui"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

func TestWireRedirectTargetCanonicalizesCategoryAndAnchor(t *testing.T) {
	tests := map[string]string{
		"":                          "/wire#community-input",
		"touchdown":                 "/wire?category=touchdown#community-input",
		" TOUCHDOWN ":               "/wire?category=touchdown#community-input",
		"news":                      "/wire?category=news#community-input",
		"//evil.example/steal":      "/wire#community-input",
		"https://evil.example/wire": "/wire#community-input",
		"category=market&x=/evil":   "/wire#community-input",
	}
	for raw, want := range tests {
		if got := wireRedirectTarget(raw); got != want {
			t.Errorf("wireRedirectTarget(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWireReturnTargetForDataUsesNormalizedCategory(t *testing.T) {
	data := map[string]any{"category": " injury "}
	if got, want := wireReturnTargetForData(data), "/wire?category=injury#community-input"; got != want {
		t.Fatalf("wireReturnTargetForData = %q, want %q", got, want)
	}
	data["category"] = "https://evil.example"
	if got, want := wireReturnTargetForData(data), "/wire#community-input"; got != want {
		t.Fatalf("hostile wireReturnTargetForData = %q, want %q", got, want)
	}
}

func TestWireActionCategoryRestoresOnlyAllowlistedQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/wire?category=old&keep=ignored", nil)
	view := action.View{Result: action.Result{Values: map[string]string{"category": " NEWS "}}}
	got := wireRequestWithActionCategory(request, view)
	if got == request {
		t.Fatal("wireRequestWithActionCategory mutated the original request")
	}
	if got.URL.Query().Get("category") != "news" || got.URL.Query().Get("keep") != "ignored" {
		t.Fatalf("restored query = %q, want sanitized category with unrelated query retained", got.URL.RawQuery)
	}
	view.Result.Values["category"] = "//evil.example"
	got = wireRequestWithActionCategory(request, view)
	if got.URL.Query().Get("category") != "" {
		t.Fatalf("hostile action category survived: %q", got.URL.RawQuery)
	}
}

func TestWireManagedSuccessUsesBoundedReturnTargetAndOmitsReservedValue(t *testing.T) {
	fallback := wireRedirectTarget(" market ")
	values := url.Values{
		action.ReturnTargetField: {"//evil.example/steal"},
		"category":               {"market"},
		"summary":                {"Starting RB1 looked limited."},
	}
	request := httptest.NewRequest(http.MethodPost, "/wire/__actions/submit-sighting", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	action.ServeHandler(response, request, func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached Wire action handler")
		}
		actionui.RedirectBackWithNotice(ctx, fallback, "Sighting added.")
		return nil
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Redirect != fallback || result.Message != "Sighting added." {
		t.Fatalf("managed result = %+v, want bounded target %q", result, fallback)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("reserved return target leaked into managed values: %#v", result.Values)
	}
}
func TestWireNativeSuccessPreservesCategoryAnchorAndFlash(t *testing.T) {
	manager := session.MustNew("wire-return-target-native-secret", session.Options{
		CookieName:    "wire_return_target_native",
		AllowInsecure: true,
	})
	target := wireRedirectTarget("injury")
	values := url.Values{
		action.ReturnTargetField: {target},
		"category":               {"injury"},
	}
	request := httptest.NewRequest(http.MethodPost, "/wire/__actions/submit-sighting", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	postResponse := httptest.NewRecorder()
	handler := manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		action.ServeHandler(writer, request, func(ctx *action.Context) error {
			actionui.RedirectBackWithNotice(ctx, wireRedirectTarget(ctx.FormData["category"]), "Sighting added.")
			return nil
		})
	}))
	handler.ServeHTTP(postResponse, request)
	if postResponse.Code != http.StatusSeeOther || postResponse.Header().Get("Location") != target {
		t.Fatalf("POST = %d %q, want 303 %q", postResponse.Code, postResponse.Header().Get("Location"), target)
	}
	cookies := postResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("POST wrote %d cookies, want one native feedback cookie", len(cookies))
	}
	getRequest := httptest.NewRequest(http.MethodGet, "/wire?category=injury", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler = manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		store := session.Current(request)
		if store == nil {
			t.Fatal("native GET missing session")
		}
		flashes := store.Flashes("notice")
		if len(flashes) != 1 || flashes[0] != "Sighting added." {
			t.Fatalf("native notice flashes = %#v", flashes)
		}
		view, ok := action.State(request, "submit-sighting")
		if !ok || view.Redirect() != target || view.Message() != "Sighting added." {
			t.Fatalf("native action view = %+v, want target/message", view)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getResponse.Code)
	}
}

func TestWireNativeValidationPreservesInvalidValuesAndCanonicalRedirect(t *testing.T) {
	manager := session.MustNew("wire-validation-native-secret", session.Options{
		CookieName:    "wire_validation_native",
		AllowInsecure: true,
	})
	values := url.Values{
		action.ReturnTargetField: {"https://evil.example/steal"},
		"category":               {"community"},
		"source_name":            {"  Local TV  "},
		"summary":                {"  A useful invalid submission  "},
	}
	request := httptest.NewRequest(http.MethodPost, "/wire/__actions/submit-sighting", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	postResponse := httptest.NewRecorder()
	handler := manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		action.ServeHandler(writer, request, func(ctx *action.Context) error {
			return wireValidationWithRedirect(ctx, wireRedirectTarget(ctx.FormData["category"]), errors.New("summary is required"))
		})
	}))
	handler.ServeHTTP(postResponse, request)
	if postResponse.Code != http.StatusSeeOther || postResponse.Header().Get("Location") != "/wire?category=community#community-input" {
		t.Fatalf("hostile validation POST = %d %q, want canonical Wire category target", postResponse.Code, postResponse.Header().Get("Location"))
	}
	cookies := postResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("validation POST wrote %d cookies, want one", len(cookies))
	}
	getRequest := httptest.NewRequest(http.MethodGet, "/wire?category=community", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler = manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		view, ok := action.State(request, "submit-sighting")
		if !ok {
			t.Fatal("native validation did not flash action state")
		}
		if view.Value("source_name") != "  Local TV  " || view.Value("summary") != "  A useful invalid submission  " {
			t.Fatalf("flashed invalid values = %#v", view.Result.Values)
		}
		if view.Error("summary") != "summary is required" {
			t.Fatalf("flashed summary error = %q", view.Error("summary"))
		}
		if _, ok := view.Result.Values[action.ReturnTargetField]; ok {
			t.Fatalf("reserved return target leaked into flashed values")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getResponse.Code)
	}
}
func TestWireManagedValidationKeepsInvalidValuesAndLocalFeedbackContract(t *testing.T) {
	values := url.Values{
		action.ReturnTargetField: {wireRedirectTarget("news")},
		"category":               {"news"},
		"source_name":            {"Local desk"},
		"summary":                {"Needs a public link"},
	}
	request := httptest.NewRequest(http.MethodPost, "/wire/__actions/submit-sighting", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	action.ServeHandler(response, request, func(ctx *action.Context) error {
		return wireValidationWithRedirect(ctx, wireRedirectTarget(ctx.FormData["category"]), errors.New("source link is required"))
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("managed validation status = %d, want 422", response.Code)
	}
	var result action.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Values["source_name"] != "Local desk" || result.Values["summary"] != "Needs a public link" {
		t.Fatalf("managed invalid values = %#v", result.Values)
	}
	if result.FieldErrors["source_url"] != "source link is required" {
		t.Fatalf("managed field errors = %#v", result.FieldErrors)
	}
	if _, ok := result.Values[action.ReturnTargetField]; ok {
		t.Fatalf("managed result leaked reserved return target")
	}
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	for _, want := range []string{
		"data-gosx-managed=\"true\"",
		"class=\"flash-message\" role=\"status\"",
		"class=\"error-message\" role=\"alert\"",
		"name={data.wire_return_target_field}",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("Wire local feedback/managed form contract missing %q", want)
		}
	}
}
