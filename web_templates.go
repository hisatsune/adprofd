package main

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func newTemplates() (*template.Template, error) {
	return template.New("").
		Funcs(templateFuncs()).
		ParseFS(templateFS, "templates/*.html")
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"urlFor": func(name string) string {
			switch name {
			case "login":
				return "/login"
			case "profile":
				return "/profile"
			case "logout":
				return "/logout"
			default:
				return "#"
			}
		},
	}
}

func (app *App) render(w http.ResponseWriter, status int, page string, data any) {
	name := page + ".html"

	var buf bytes.Buffer

	if err := app.templates.ExecuteTemplate(&buf, name, data); err != nil {
		app.logger.Error("execute template failed",
			"template", name,
			"error", err,
		)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if _, err := buf.WriteTo(w); err != nil {
		app.logger.Error("write response failed",
			"template", name,
			"error", err,
		)
	}
}
