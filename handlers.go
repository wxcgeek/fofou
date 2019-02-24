package main

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kjk/u"
)

type ForumInfo struct {
	ForumFullURL string
	ForumTitle   string
}

// url: /{forum}/viewraw?topicId=${topicId}&postId=${postId}
func handleViewRaw(forum *Forum, w http.ResponseWriter, r *http.Request) {
	topicID, _ := strconv.Atoi(r.FormValue("tid"))
	postID, _ := strconv.Atoi(r.FormValue("pid"))
	topic := forum.Store.TopicByID(uint32(topicID))
	if nil == topic {
		logger.Noticef("handleViewRaw(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
	post := topic.Posts[postID-1]
	msg := post.Message()
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("****** Raw:\n"))
	w.Write([]byte(msg))
	w.Write([]byte("\n\n****** Converted:\n"))
	w.Write([]byte(msgToHtml(msg)))
}

func serveFileFromDir(w http.ResponseWriter, r *http.Request, dir, fileName string) {
	filePath := filepath.Join(dir, fileName)
	if !u.PathExists(filePath) {
		logger.Noticef("serveFileFromDir() file %q doesn't exist, referer: %q", fileName, getReferer(r))
	}
	http.ServeFile(w, r, filePath)
}

// url: /s/*
func handleStatic(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Path[len("/s/"):]
	serveFileFromDir(w, r, "static", file)
}

// url: /robots.txt
func handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	serveFileFromDir(w, r, "static", "robots.txt")
}

// url: /{forum}/postdel?topicId=${topicId}&postId=${postId}
func handlePostDelete(forum *Forum, w http.ResponseWriter, r *http.Request) {
	if forum.IsAdmin(getSecureCookie(r)) {
		topicID, _ := strconv.Atoi(r.FormValue("tid"))
		postID, _ := strconv.Atoi(r.FormValue("pid"))
		forum.Store.DeletePost(uint32(topicID), uint16(postID))
		http.Redirect(w, r, fmt.Sprintf("/%s?tid=%d", forum.ForumUrl, topicID), 302)
		return
	}
	w.WriteHeader(403)
}

// url: /{forum}/block?term=${term}&action=[bu]
func handleBlock(forum *Forum, w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.FormValue("ip"))
	user := strings.TrimSpace(r.FormValue("u"))
	if user == "" && ip == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s", forum.ForumUrl), 302)
		return
	}
	if forum.IsAdmin(getSecureCookie(r)) {
		if ip != "" {
			forum.Store.BlockIP(ip)
			http.Redirect(w, r, fmt.Sprintf("/%s?t=list&ip=%s", forum.ForumUrl, ip), 302)
		} else {
			forum.Store.BlockUser(user)
			http.Redirect(w, r, fmt.Sprintf("/%s?t=list&u=%s", forum.ForumUrl, user), 302)
		}
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
}

// url: /{forum}/taction?topicId=${topicId}&action=[slx]
func handleOperateTopic(forum *Forum, w http.ResponseWriter, r *http.Request) {
	topicID, _ := strconv.Atoi(r.FormValue("tid"))
	redirect := r.FormValue("redirect")

	if forum.IsAdmin(getSecureCookie(r)) {
		switch r.FormValue("t") {
		case "stick":
			forum.Store.OperateTopic(uint32(topicID), OP_STICKY)
		case "lock":
			forum.Store.OperateTopic(uint32(topicID), OP_LOCK)
		case "purge":
			forum.Store.OperateTopic(uint32(topicID), OP_PURGE)
		}
		if redirect == "" {
			http.Redirect(w, r, "/"+forum.ForumUrl, 302)
		} else {
			http.Redirect(w, r, redirect, 302)
		}
		return
	}
	w.WriteHeader(403)
}

// url: /
func handleMain(w http.ResponseWriter, r *http.Request) {
	if !isTopLevelURL(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	model := struct {
		Forums *[]*Forum
		Title  string
		Forum
		LogInOut template.HTML
	}{
		Forums: &appState.Forums,
	}
	ExecTemplate(w, tmplMain, model)
}

func isTopLevelURL(url string) bool {
	return 0 == len(url) || "/" == url
}

// // https://blog.gopheracademy.com/advent-2016/exposing-go-on-the-internet/
func initHTTPServer() *http.Server {
	smux := &http.ServeMux{}
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/logs", handleLogs)
	smux.HandleFunc("/s/", makeTimingHandler(handleStatic))
	smux.HandleFunc("/", makeTimingHandler(handleForum))
	return &http.Server{Handler: smux}
}
