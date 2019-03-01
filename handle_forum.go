// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/coyove/fofou/server"
)

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

	nTopicsMax := 15
	isAdmin := forum.IsAdmin(getUser(r).ID)
	withDeleted := isAdmin
	topics, newFrom := forum.Store.GetTopics(nTopicsMax, from, withDeleted)
	prevTo := from - nTopicsMax
	if prevTo < 0 {
		prevTo = 0
	}

	topicsDisplay := make([]Topic, 0)

	for _, t := range topics {
		if t.IsDeleted() && !isAdmin {
			continue
		}
		t.T_TotalPosts = uint16(len(t.Posts) - 1)
		t.T_IsAdmin = isAdmin
		t.T_IsExpand = true
		if len(t.Posts) >= 5 {
			t.Posts = t.Posts[len(t.Posts)-5:]
		}
		topicsDisplay = append(topicsDisplay, t)
	}

	model := struct {
		Forum
		NewFrom int
		PrevTo  int
		TopicID int
		Topics  []Topic
	}{
		Forum:   *forum,
		Topics:  topicsDisplay,
		NewFrom: newFrom,
		PrevTo:  prevTo,
	}

	server.Render(w, server.TmplForum, model)
}
