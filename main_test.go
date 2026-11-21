package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func request(h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRoutes(t *testing.T) {
	h := NewApp().Handler()
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"GET", "/about", 200}, {"GET", "/static/style.css", 200},
		{"GET", "/missing", 404}, {"POST", "/", 405}, {"POST", "/about", 405},
		{"GET", "/feedback", 405}, {"PUT", "/feedback", 405},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := request(h, tc.method, tc.path, nil)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
		})
	}
}

func assertRedirect(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("expected redirect to /; got %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestMalformedForm(t *testing.T) {
	h := NewApp().Handler()
	r := httptest.NewRequest("POST", "/feedback", strings.NewReader("x=%zz"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("got %d", w.Code)
	}
}

// Реальные одновременные запросы проверяют защиту состояния mutex-ом.
func TestConcurrentAdds(t *testing.T) {
	a := NewApp()
	h := a.Handler()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			form := url.Values{"author": {fmt.Sprint(i)}, "message": {"Отзыв"}}
			if w := request(h, "POST", "/feedback", form); w.Code != 303 {
				t.Errorf("status %d", w.Code)
			}
			request(h, "GET", "/", nil)
		}(i)
	}
	wg.Wait()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.feedbacks) != 24 {
		t.Fatalf("count %d", len(a.feedbacks))
	}
	seen := map[int]bool{}
	for _, item := range a.feedbacks {
		if seen[item.ID] {
			t.Fatalf("duplicate ID %d", item.ID)
		}
		seen[item.ID] = true
	}
}

func TestFeedbackFlow(t *testing.T) {
	h := NewApp().Handler()
	for _, form := range []url.Values{
		{"author": {""}, "message": {"Text"}}, {"author": {"Name"}, "message": {" "}}, {"author": {"  "}, "message": {"Text"}},
	} {
		if w := request(h, "POST", "/feedback", form); w.Code != 400 {
			t.Fatalf("invalid form: %d", w.Code)
		}
	}
	if w := request(h, "GET", "/", nil); !strings.Contains(w.Body.String(), "Всего отзывов: 0") {
		t.Fatal("invalid forms changed data")
	}
	for i := 1; i <= 2; i++ {
		assertRedirect(t, request(h, "POST", "/feedback", url.Values{"author": {" Автор "}, "message": {fmt.Sprintf("<script>alert(%d)</script>", i)}}))
	}
	for i := 0; i < 2; i++ {
		body := request(h, "GET", "/", nil).Body.String()
		if !strings.Contains(body, "Всего отзывов: 2") || !strings.Contains(body, "#2 · Автор") {
			t.Fatal("feedbacks/count not preserved")
		}
		if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
			t.Fatal("HTML not escaped")
		}
	}
}
