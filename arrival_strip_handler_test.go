package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeArrivalStripDismisser struct {
	err   error
	calls int
}

func (f *fakeArrivalStripDismisser) DismissArrivalStrip(r *http.Request) error {
	f.calls++
	return f.err
}

// TestArrivalStripDismissHandlerRejectsNonGET pins the handler's one
// accepted method: J5 F37's dismiss control is a plain <a href>, never a
// form, so the handler only ever receives GET.
func TestArrivalStripDismissHandlerRejectsNonGET(t *testing.T) {
	svc := &fakeArrivalStripDismisser{}
	handler := arrivalStripDismissHandler(svc)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/arrival-strip/dismiss", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if svc.calls != 0 {
		t.Fatalf("DismissArrivalStrip called %d times for a rejected method, want 0", svc.calls)
	}
}

// TestArrivalStripDismissHandlerRedirectsHomeOnSuccess pins the happy
// path: a signed-in GET dismisses and lands back on the home page.
func TestArrivalStripDismissHandlerRedirectsHomeOnSuccess(t *testing.T) {
	svc := &fakeArrivalStripDismisser{}
	handler := arrivalStripDismissHandler(svc)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/arrival-strip/dismiss", nil))
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want \"/\"", got)
	}
	if svc.calls != 1 {
		t.Fatalf("DismissArrivalStrip called %d times, want 1", svc.calls)
	}
}

// TestArrivalStripDismissHandlerSendsAnUnsignedViewerToLogin pins the
// unsigned path: DismissArrivalStrip's own "Google sign-in is required"
// error routes to /login rather than a raw 401.
func TestArrivalStripDismissHandlerSendsAnUnsignedViewerToLogin(t *testing.T) {
	svc := &fakeArrivalStripDismisser{err: errors.New("Google sign-in is required")}
	handler := arrivalStripDismissHandler(svc)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/arrival-strip/dismiss", nil))
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if got := recorder.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want \"/login\"", got)
	}
}
