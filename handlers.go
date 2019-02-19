package main

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/kjk/u"
)

type ForumInfo struct {
	ForumFullURL string
	ForumTitle   string
}

// url: /{forum}/viewraw?topicId=${topicId}&postId=${postId}
func handleViewRaw(w http.ResponseWriter, r *http.Request) {
	forum, topicID, postID := getTopicAndPostID(w, r)
	if 0 == topicID {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
	topic := forum.Store.TopicByID(uint32(topicID))
	if nil == topic {
		logger.Noticef("handleViewRaw(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
	post := topic.Posts[postID-1]
	msg := post.Message
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

func getTopicAndPostID(w http.ResponseWriter, r *http.Request) (*Forum, uint32, uint16) {
	forum := mustGetForum(w, r)
	if forum == nil {
		http.Redirect(w, r, "/", 302)
		return nil, 0, 0
	}
	topicIDStr := strings.TrimSpace(r.FormValue("topicId"))
	postIDStr := strings.TrimSpace(r.FormValue("postId"))
	topicID, err := strconv.Atoi(topicIDStr)
	if err != nil || topicID == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return nil, 0, 0
	}
	postID, err := strconv.Atoi(postIDStr)
	if err != nil || postID == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return forum, 0, 0
	}
	return forum, uint32(topicID), uint16(postID)
}

// url: /{forum}/postdel?topicId=${topicId}&postId=${postId}
func handlePostDelete(w http.ResponseWriter, r *http.Request) {
	if forum, topicID, postID := getTopicAndPostID(w, r); forum != nil {
		//fmt.Printf("handlePostDelete(): forum: %q, topicId: %d, postId: %d\n", forum.ForumUrl, topicId, postId)
		// TODO: handle error?
		if userIsAdmin(forum, getSecureCookie(r)) {
			forum.Store.DeletePost(topicID, postID)
			http.Redirect(w, r, fmt.Sprintf("/%s/topic?id=%d", forum.ForumUrl, topicID), 302)
			return
		}
	}
	w.WriteHeader(403)
}

// url: /{forum}/block?term=${term}&action=[bu]
func handleBlock(w http.ResponseWriter, r *http.Request) {
	forum := mustGetForum(w, r)
	if forum == nil {
		w.WriteHeader(403)
		return
	}
	action := r.FormValue("action")
	term := strings.TrimSpace(r.FormValue("term"))
	if term == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
		return
	}
	if userIsAdmin(forum, getSecureCookie(r)) {
		if action == "b" {
			forum.Store.BlockIP(term)
			http.Redirect(w, r, fmt.Sprintf("/%s/postsby?ip=%s", forum.ForumUrl, term), 302)
			return
		} else if action == "u" {
			forum.Store.BlockUser(term)
			http.Redirect(w, r, fmt.Sprintf("/%s/postsby?user=%s", forum.ForumUrl, term), 302)
			return
		}
	}
	w.WriteHeader(403)
}

// url: /{forum}/taction?topicId=${topicId}&action=[slx]
func handleActionTopic(w http.ResponseWriter, r *http.Request) {
	topicID, err := strconv.Atoi(r.FormValue("topicId"))
	action := r.FormValue("action")
	redirect := r.FormValue("redirect")
	forum := mustGetForum(w, r)
	if forum != nil && err == nil {
		if userIsAdmin(forum, getSecureCookie(r)) {
			forum.Store.DoAction(uint32(topicID), strings.ToUpper(action[:1])[0])
			if redirect == "" {
				http.Redirect(w, r, "/"+forum.ForumUrl, 302)
			} else {
				http.Redirect(w, r, redirect, 302)
			}
			return
		}
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
		Forums:   &appState.Forums,
		Title:    config.MainTitle,
		LogInOut: getLogInOut(r, getSecureCookie(r)),
	}
	ExecTemplate(w, tmplMain, model)
}

func isTopLevelURL(url string) bool {
	return 0 == len(url) || "/" == url
}

// // https://blog.gopheracademy.com/advent-2016/exposing-go-on-the-internet/
func initHTTPServer() *http.Server {
	r := mux.NewRouter()
	r.HandleFunc("/", makeTimingHandler(handleMain))
	r.HandleFunc("/{forum}", makeTimingHandler(handleForum))
	r.HandleFunc("/{forum}/", makeTimingHandler(handleForum))
	r.HandleFunc("/{forum}/rss.xml", makeTimingHandler(handleRss))
	r.HandleFunc("/{forum}/topic", makeTimingHandler(handleTopic))
	r.HandleFunc("/{forum}/postsby", makeTimingHandler(handlePostsBy))
	r.HandleFunc("/{forum}/viewraw", makeTimingHandler(handleViewRaw))
	r.HandleFunc("/{forum}/newpost", makeTimingHandler(handleNewPost))
	r.HandleFunc("/{forum}/block", makeTimingHandler(handleBlock))
	r.HandleFunc("/{forum}/delete", makeTimingHandler(handlePostDelete))
	r.HandleFunc("/{forum}/taction", makeTimingHandler(handleActionTopic))

	smux := &http.ServeMux{}
	smux.HandleFunc("/oauthgithubcb", handleOauthGithubCallback)
	smux.HandleFunc("/login", handleLogin)
	smux.HandleFunc("/logout", handleLogout)
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/logs", handleLogs)
	smux.HandleFunc("/s/", makeTimingHandler(handleStatic))
	smux.Handle("/", r)

	srv := &http.Server{
		// TODO: 1.8 only
		// IdleTimeout:  120 * time.Second,
		Handler: smux,
	}
	// TODO: track connections and their state
	return srv
}
