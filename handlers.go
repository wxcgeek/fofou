package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/coyove/fofou/server"
)

type ForumInfo struct {
	ForumFullURL string
	ForumTitle   string
}

func serveFileFromDir(w http.ResponseWriter, r *http.Request, dir, fileName string) {
	filePath := filepath.Join(dir, fileName)
	http.ServeFile(w, r, filePath)
}

// url: /s/*
func handleStatic(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/s/"):]
	serveFileFromDir(w, r, "static", file)
}

func handleImage(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/i/"):]
	serveFileFromDir(w, r, "data/images", file)
}

// url: /robots.txt
func handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	serveFileFromDir(w, r, "static", "robots.txt")
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	if !server.GetUser(r).IsAdmin() {
		w.WriteHeader(403)
		return
	}

	m := &runtime.MemStats{}
	runtime.ReadMemStats(m)

	model := struct {
		server.Forum
		Errors  []*server.TimestampedMsg
		Notices []*server.TimestampedMsg
		Header  *http.Header
		runtime.MemStats
	}{
		Forum:    *forum,
		MemStats: *m,
		Errors:   forum.GetErrors(),
		Notices:  forum.GetNotices(),
		Header:   &r.Header,
	}

	server.Render(w, server.TmplLogs, model)
}

func preHandle(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !forum.IsReady() {
			w.Write([]byte(fmt.Sprintf("%v Booting... %.1f%%", time.Now().Format(time.RFC1123), forum.LoadingProgress()*100)))
			return
		}

		startTime := time.Now()

		fn(w, r)

		duration := time.Now().Sub(startTime)
		if duration.Seconds() > 0.1 {
			url := r.URL.Path
			if len(r.URL.RawQuery) > 0 {
				url = fmt.Sprintf("%s?%s", url, r.URL.RawQuery)
			}
			forum.Notice("%q took %f seconds to serve", url, duration.Seconds())
		}
	}
}

func initHTTPServer() *http.Server {
	smux := &http.ServeMux{}
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/logs", preHandle(handleLogs))
	smux.HandleFunc("/s/", preHandle(handleStatic))
	smux.HandleFunc("/i/", preHandle(handleImage))
	smux.HandleFunc("/api", preHandle(handleNewPost))
	smux.HandleFunc("/list", preHandle(handleList))
	smux.HandleFunc("/t/", preHandle(handleTopic))
	smux.HandleFunc("/", preHandle(handleTopics))
	return &http.Server{Handler: smux}
}
