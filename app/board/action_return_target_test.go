package board

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
)

func TestBoardAddExplicitSuccessRedirectWinsOverReturnTarget(t *testing.T) {
	returnTarget := boardRedirectTarget("WR", "Tom & /", "3")
	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"player_id":              {"player-1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/board/__actions/board-add", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	action.ServeHandler(response, request, func(ctx *action.Context) error {
		if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached the Board add handler")
		}
		ctx.Redirect("/board?success=1#board-pool")
		return nil
	})

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if got := response.Header().Get("Location"); got != "/board?success=1#board-pool" {
		t.Fatalf("Location = %q, want explicit success redirect", got)
	}
}

func TestBoardAddNativeValidationPreservesCanonicalContextAndOmitsReservedValue(t *testing.T) {
	returnTarget := boardRedirectTarget("WR", "Tom & /", "3")
	manager := session.MustNew("board-return-target-native-secret", session.Options{
		CookieName:    "board_return_target_native",
		AllowInsecure: true,
	})
	handler := manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			action.ServeHandler(writer, request, func(ctx *action.Context) error {
				if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
					t.Fatal("reserved return target reached the Board add handler")
				}
				return action.Validation(
					"choose a player",
					map[string]string{"player_id": "choose a player"},
					ctx.FormData,
				)
			})
			return
		}

		view, ok := action.State(request, "board-add")
		if !ok {
			t.Fatal("native validation did not flash Board add state")
		}
		if got := view.Value("q"); got != "Tom & /" {
			t.Fatalf("flashed query = %q, want original search", got)
		}
		if got := view.Value("pos"); got != "WR" {
			t.Fatalf("flashed position = %q, want WR", got)
		}
		if got := view.Value("page"); got != "3" {
			t.Fatalf("flashed page = %q, want 3", got)
		}
		if _, ok := view.Result.Values[action.ReturnTargetField]; ok {
			t.Fatal("reserved return target reached flashed action values")
		}
		if got := view.Error("player_id"); got != "choose a player" {
			t.Fatalf("flashed player error = %q", got)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	values := url.Values{
		action.ReturnTargetField: {returnTarget},
		"player_id":              {"player-1"},
		"pos":                    {"WR"},
		"q":                      {"Tom & /"},
		"page":                   {"3"},
	}
	request := httptest.NewRequest(http.MethodPost, "/board/__actions/board-add", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, request)

	if postResponse.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", postResponse.Code)
	}
	if got := postResponse.Header().Get("Location"); got != returnTarget {
		t.Fatalf("POST Location = %q, want canonical return target %q", got, returnTarget)
	}
	cookies := postResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("POST wrote %d session cookies, want one flashed state cookie", len(cookies))
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/board?page=3&pos=WR&q=Tom+%26+%2F", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getResponse.Code)
	}
}

func TestBoardAddHostileReturnTargetFallsBackToActionRouteAndNeverFlashesReservedValue(t *testing.T) {
	manager := session.MustNew("board-return-target-hostile-secret", session.Options{
		CookieName:    "board_return_target_hostile",
		AllowInsecure: true,
	})
	handler := manager.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			action.ServeHandler(writer, request, func(ctx *action.Context) error {
				if _, ok := ctx.FormData[action.ReturnTargetField]; ok {
					t.Fatal("reserved hostile return target reached the Board add handler")
				}
				return action.Validation(
					"choose a player",
					map[string]string{"player_id": "choose a player"},
					ctx.FormData,
				)
			})
			return
		}

		view, ok := action.State(request, "board-add")
		if !ok {
			t.Fatal("hostile validation did not flash Board add state")
		}
		if _, ok := view.Result.Values[action.ReturnTargetField]; ok {
			t.Fatal("reserved hostile return target reached flashed action values")
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	values := url.Values{
		action.ReturnTargetField: {"//evil.example/steal"},
		"player_id":              {"player-1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/board/__actions/board-add", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, request)

	if postResponse.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303", postResponse.Code)
	}
	if got := postResponse.Header().Get("Location"); got != "/board" {
		t.Fatalf("hostile return target Location = %q, want action route fallback /board", got)
	}
	cookies := postResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("POST wrote %d session cookies, want one flashed state cookie", len(cookies))
	}
	getRequest := httptest.NewRequest(http.MethodGet, "/board", nil)
	getRequest.AddCookie(cookies[0])
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusNoContent {
		t.Fatalf("GET status = %d, want 204", getResponse.Code)
	}
}
