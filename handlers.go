package main

import (
	"net/http"
	"path/filepath"

	"github.com/kjk/u"
)

type ForumInfo struct {
	ForumFullURL string
	ForumTitle   string
}

// url: /{forum}/viewraw?topicId=${topicId}&postId=${postId}
func handleViewRaw(forum *Forum, w http.ResponseWriter, r *http.Request) {
	//topicID, _ := strconv.Atoi(r.FormValue("tid"))
	//postID, _ := strconv.Atoi(r.FormValue("pid"))
	//topic := forum.Store.TopicByID(uint32(topicID))
	//if nil == topic {
	//	logger.Noticef("handleViewRaw(): didn't find topic with id %d, referer: %q", topicID, getReferer(r))
	//	http.Redirect(w, r, fmt.Sprintf("/%s/", forum.ForumUrl), 302)
	//	return
	//}
	//post := topic.Posts[postID-1]
	//msg := post.Message()
	//w.Header().Set("Content-Type", "text/plain")
	//w.Write([]byte("****** Raw:\n"))
	//w.Write([]byte(msg))
	//w.Write([]byte("\n\n****** Converted:\n"))
	//w.Write([]byte(msgToHtml(msg)))
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

// // https://blog.gopheracademy.com/advent-2016/exposing-go-on-the-internet/
func initHTTPServer() *http.Server {
	smux := &http.ServeMux{}
	smux.HandleFunc("/favicon.ico", http.NotFound)
	smux.HandleFunc("/robots.txt", handleRobotsTxt)
	smux.HandleFunc("/logs", handleLogs)
	smux.HandleFunc("/s/", makeTimingHandler(handleStatic))
	//smux.HandleFunc("/api", makeTimingHandler(handleNewPost))
	smux.HandleFunc("/", makeTimingHandler(handleForum))
	return &http.Server{Handler: smux}
}
