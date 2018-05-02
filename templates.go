package main

import (
	"bytes"
	"net/http"
	"path/filepath"
	"sync"
	"text/template"
	"time"
)

var (
	tmplMain      = "main.html"
	tmplForum     = "forum.html"
	tmplTopic     = "topic.html"
	tmplPosts     = "posts.html"
	tmplNewPost   = "newpost.html"
	tmplLogs      = "logs.html"
	templateNames = [...]string{tmplMain, tmplForum, tmplTopic, tmplPosts, tmplNewPost, tmplLogs, "footer.html", "header.html", "forumnav.html"}
	templatePaths []string
	templates     *template.Template
	tmplMutex     sync.RWMutex
)

func init() {
	for _, name := range templateNames {
		templatePaths = append(templatePaths, filepath.Join("tmpl", name))
	}

	go func() {
		for range time.Tick(time.Second * 2) {
			tmplMutex.Lock()
			templates = template.Must(template.ParseFiles(templatePaths...))
			tmplMutex.Unlock()
		}
	}()
}

func ExecTemplate(w http.ResponseWriter, templateName string, model interface{}) bool {
	tmplMutex.RLock()

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, templateName, model); err != nil {
		logger.Errorf("failed to execute template %q, error: %s", templateName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		tmplMutex.RUnlock()
		return false
	}

	w.Write(buf.Bytes())
	tmplMutex.RUnlock()
	return true
}
