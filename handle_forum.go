// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"fmt"
	"html/template"
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

func plural(n int, s string) string {
	if 1 == n {
		return fmt.Sprintf("%d %s", n, s)
	}
	return fmt.Sprintf("%d %ss", n, s)
}

func (p *TopicDisplay) CreatedOnStr() string {
	return prettySec(p.CreatedOn)
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

// url: /{forum}
func handleForum(w http.ResponseWriter, r *http.Request) {
	forum := mustGetForum(w, r)
	if forum == nil {
		return
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
	isAdmin := userIsAdmin(forum, cookie)
	withDeleted := isAdmin
	topics, newFrom := forum.Store.GetTopics(nTopicsMax, from, withDeleted)
	prevTo := from - nTopicsMax
	if prevTo < 0 {
		prevTo = -1
	}

	topicsDisplay := [][]*TopicDisplay{
		make([]*TopicDisplay, 0),
		make([]*TopicDisplay, 0),
	}

	for i, t := range topics {
		if t.IsDeleted() && !isAdmin {
			continue
		}
		d := &TopicDisplay{
			Topic:     *t,
			CreatedBy: t.Posts[0].UserName(),
		}
		nComments := len(t.Posts) - 1
		d.CommentsCount = nComments
		d.No = 1 + i + from
		if t.IsDeleted() {
			d.TopicLinkClass = "deleted"
		}
		if 0 == nComments {
			d.TopicURL = fmt.Sprintf("/%s/topic?id=%d", forum.ForumUrl, t.ID)
		} else {
			d.TopicURL = fmt.Sprintf("/%s/topic?id=%d&comments=%d", forum.ForumUrl, t.ID, nComments)
		}

		if i < nTopicsMax/2 {
			topicsDisplay[0] = append(topicsDisplay[0], d)
		} else {
			topicsDisplay[1] = append(topicsDisplay[1], d)
		}
	}

	model := struct {
		Forum
		NewFrom  int
		PrevTo   int
		IsAdmin  bool
		Topics   [][]*TopicDisplay
		LogInOut template.HTML
	}{
		Forum:    *forum,
		Topics:   topicsDisplay,
		NewFrom:  newFrom,
		PrevTo:   prevTo,
		LogInOut: getLogInOut(r, getSecureCookie(r)),
		IsAdmin:  isAdmin,
	}

	ExecTemplate(w, tmplForum, model)
}
