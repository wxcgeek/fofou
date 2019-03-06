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
	TmplForum   = "forum.html"
	TmplTopic   = "topic.html"
	TmplTopic1  = "topic1.html"
	TmplPosts   = "list.html"
	TmplNewPost = "newpost.html"
	TmplLogs    = "logs.html"
	TmplHelp    = "help.html"
	TmplFooter  = "footer.html"
)

var (
	templateNames = []string{TmplForum, TmplTopic, TmplTopic1, TmplPosts, TmplNewPost, TmplLogs, TmplFooter, TmplHelp, "header.html", "forumnav.html", "post1.html"}
	templatePaths []string
	templates     *template.Template
	tmplMutex     sync.RWMutex
)

func LoadTemplates(prod bool) {

	for _, name := range templateNames {
		templatePaths = append(templatePaths, filepath.Join("tmpl", name))
	}

	templates = template.Must(template.ParseFiles(templatePaths...))
	go func() {
		tick := 2
		if prod {
			tick = 10
		}
		for range time.Tick(time.Second * time.Duration(tick)) {
			tmplMutex.Lock()
			t, err := template.ParseFiles(templatePaths...)
			if err == nil {
				templates = t
			}
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
