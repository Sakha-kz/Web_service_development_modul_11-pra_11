package main

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
)

//go:embed templates/*.html static/*
var assets embed.FS

// App хранит состояние одного экземпляра сервера.
// Mutex защищает срез: HTTP-сервер обрабатывает запросы параллельно.
type App struct {
	mu        sync.Mutex
	feedbacks []Feedback
	nextID    int
	templates *template.Template
}

func NewApp() *App {
	return &App{nextID: 1, templates: template.Must(template.ParseFS(assets, "templates/*.html"))}
}

// Handler создаёт отдельный маршрутизатор; неизвестные адреса возвращают 404.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.homeHandler)
	mux.HandleFunc("GET /about", a.aboutHandler)
	mux.HandleFunc("POST /feedback", a.feedbackHandler)
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	return mux
}

// render сначала заполняет буфер, чтобы при ошибке не отправить половину страницы.
func (a *App) render(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("template: %v", err)
		http.Error(w, "Ошибка формирования страницы", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (a *App) aboutHandler(w http.ResponseWriter, r *http.Request) {
	a.render(w, "about.html", nil)
}

// Ограничение относится к телу формы, чтобы сервер не читал неограниченный ввод.
func parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Некорректная или слишком большая форма", http.StatusBadRequest)
		return false
	}
	return true
}

func main() {
	app := NewApp()
	log.Println("Feedback Collector: http://localhost:8080")
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", app.Handler()))
}

// Feedback соответствует структуре из практического задания.
type Feedback struct {
	ID      int
	Author  string
	Message string
}

type PageData struct {
	Feedbacks []Feedback
	Total     int
}

func (a *App) homeHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	data := PageData{Feedbacks: append([]Feedback(nil), a.feedbacks...), Total: len(a.feedbacks)}
	a.mu.Unlock()
	a.render(w, "index.html", data)
}

func (a *App) feedbackHandler(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	// FormValue читает поля HTML-формы; TrimSpace исключает ввод из одних пробелов.
	author := strings.TrimSpace(r.FormValue("author"))
	message := strings.TrimSpace(r.FormValue("message"))
	if author == "" || message == "" {
		http.Error(w, "Имя автора и текст отзыва не должны быть пустыми", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	a.feedbacks = append(a.feedbacks, Feedback{ID: a.nextID, Author: author, Message: message})
	a.nextID++
	a.mu.Unlock()
	// POST → Redirect → GET: обновление страницы не повторяет отправку формы.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
