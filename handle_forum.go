// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// TopicDisplay describes a topic
type TopicDisplay struct {
	Topic
	CommentsCount  int
	No             int
	CreatedBy      string
	TopicLinkClass string
	TopicURL       string
}

// those happen often so exclude them in order to not overwhelm the logs
var skipForums = []string{"fofou", "topic.php", "post", "newpost",
	"crossdomain.xml", "azenv.php", "index.php"}

func logMissingForum(forumURL, referer string) bool {
	if referer == "" {
		return false
	}
	for _, forum := range skipForums {
		if forum == forumURL {
			return false
		}
	}
	return true
}

func mustGetForum(w http.ResponseWriter, r *http.Request) *Forum {
	vars := mux.Vars(r)
	forumURL := vars["forum"]
	if forum := findForum(forumURL); forum != nil {
		return forum
	}

	if logMissingForum(forumURL, getReferer(r)) {
		logger.Noticef("didn't find forum %q, referer: %q", forumURL, getReferer(r))
	}
	httpErrorf(w, "Forum %q doesn't exist", forumURL)
	return nil
}

func handleForum(w http.ResponseWriter, r *http.Request) {
	if isTopLevelURL(r.URL.Path) {
		handleMain(w, r)
		return
	}

	forumName := r.URL.Path[1:]
	if idx := strings.Index(forumName, "/"); idx > -1 {
		forumName = forumName[:idx]
	}

	forum := findForum(forumName)
	if forum == nil {
		if logMissingForum(forumName, getReferer(r)) {
			logger.Noticef("didn't find forum %q, referer: %q", forumName, getReferer(r))
		}
		httpErrorf(w, "Forum %q doesn't exist", forumName)
		return
	}

	switch r.FormValue("t") {
	case "new":
		handleNewPost(forum, w, r)
		return
	case "list":
		handleList(forum, w, r)
		return
	case "block":
		handleBlock(forum, w, r)
		return
	case "del":
		handlePostDelete(forum, w, r)
		return
	case "raw":
		handleViewRaw(forum, w, r)
		return
	case "rss":
		handleRSS(forum, w, r)
		return
	case "stick", "lock", "purge":
		handleOperateTopic(forum, w, r)
		return
	default:
		topicID, _ := strconv.Atoi(r.FormValue("tid"))
		if topicID > 0 {
			handleTopic(forum, topicID, w, r)
			return
		}
	}

	fromStr := strings.TrimSpace(r.FormValue("from"))
	from := 0
	if "" != fromStr {
		var err error
		if from, err = strconv.Atoi(fromStr); err != nil {
			from = 0
		}
	}
	//fmt.Printf("handleForum(): forum: %q, from: %d\n", forum.ForumUrl, from)

	nTopicsMax := 50
	cookie := getSecureCookie(r)
	isAdmin := forum.IsAdmin(cookie)
	withDeleted := isAdmin
	topics, newFrom := forum.Store.GetTopics(nTopicsMax, from, withDeleted)
	prevTo := from - nTopicsMax
	if prevTo < 0 {
		prevTo = -1
	}

	topicsDisplay := make([]*TopicDisplay, 0)

	for i, t := range topics {
		if t.IsDeleted() && !isAdmin {
			continue
		}
		d := &TopicDisplay{
			Topic:     *t,
			CreatedBy: t.Posts[0].User,
		}
		nComments := len(t.Posts) - 1
		d.CommentsCount = nComments
		d.No = 1 + i + from
		if t.IsDeleted() {
			d.TopicLinkClass = "deleted"
		}

		topicsDisplay = append(topicsDisplay, d)
	}

	model := struct {
		Forum
		NewFrom int
		PrevTo  int
		IsAdmin bool
		Topics  []*TopicDisplay
	}{
		Forum:   *forum,
		Topics:  topicsDisplay,
		NewFrom: newFrom,
		PrevTo:  prevTo,
		IsAdmin: isAdmin,
	}

	ExecTemplate(w, tmplForum, model)
}
