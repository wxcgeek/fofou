// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"
	"strings"
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

func handleForum(w http.ResponseWriter, r *http.Request) {
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
	isAdmin := forum.IsAdmin(getUser(r).ID)
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
			CreatedBy: t.Posts[0].User(),
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
		TopicID int
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
