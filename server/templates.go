package server

import (
	"bytes"
	"fmt"
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
	templateNames = []string{TmplForum, TmplTopic, TmplTopic1, TmplPosts, TmplNewPost, TmplLogs, TmplFooter, TmplHelp, "header.html", "post1.html"}
	templatePaths []string
	templates     *template.Template
	tmplMutex     sync.RWMutex
)

func LoadTemplates(prod bool) {

	for _, name := range templateNames {
		templatePaths = append(templatePaths, filepath.Join("tmpl", name))
	}

	m := template.FuncMap{
		"formatBytes": func(b uint64) string {
			return fmt.Sprintf("%.2f MB", float64(b)/1024/1024)
		},
	}
	templates = template.Must(template.New("").Funcs(m).ParseFiles(templatePaths...))

	go func() {
		tick := 2
		if prod {
			tick = 10
		}
		for range time.Tick(time.Second * time.Duration(tick)) {
			tmplMutex.Lock()
			t, err := template.New("").Funcs(m).ParseFiles(templatePaths...)
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
