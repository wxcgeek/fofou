package server

import (
	"bytes"
	"net/http"
	"path/filepath"
	"sync"
	"text/template"
	"time"
)

var (
	TmplMain    = "main.html"
	TmplForum   = "forum.html"
	TmplTopic   = "topic.html"
	TmplPosts   = "posts.html"
	TmplNewPost = "newpost.html"
	TmplLogs    = "logs.html"
)

var (
	templateNames = []string{TmplMain, TmplForum, TmplTopic, TmplPosts, TmplNewPost, TmplLogs, "footer.html", "header.html", "forumnav.html", "topic1.html", "post1.html"}
	templatePaths []string
	templates     *template.Template
	tmplMutex     sync.RWMutex
)

func init() {
	for _, name := range templateNames {
		templatePaths = append(templatePaths, filepath.Join("tmpl", name))
	}

	templates = template.Must(template.ParseFiles(templatePaths...))
	go func() {
		for range time.Tick(time.Second * 2) {
			tmplMutex.Lock()
			templates = template.Must(template.ParseFiles(templatePaths...))
			tmplMutex.Unlock()
		}
	}()
}

func Render(w http.ResponseWriter, templateName string, model interface{}) {
	tmplMutex.RLock()
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, templateName, model); err != nil {
		tmplMutex.RUnlock()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmplMutex.RUnlock()
	w.Write(buf.Bytes())
}
