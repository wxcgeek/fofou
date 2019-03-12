// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/coyove/fofou/server"
)

type NewPost struct {
	TopicID   int
	PostToken string
}

// url: /t/{tid}
func handleTopic(w http.ResponseWriter, r *http.Request) {
	topicID, _ := strconv.Atoi(r.URL.Path[len("/t/"):])
	topic := forum.Store.TopicByID(uint32(topicID))
	if topic.ID == 0 {
		var err error
		topic, err = forum.LoadArchivedTopic(uint32(topicID))
		if err == nil {
			topic.Archived = true
			goto NEXT
		}

		forum.Notice("can't find topic with id %d, referer: %q, err: %v", topicID, r.Referer(), err)
		http.Redirect(w, r, "/", 302)
		return
	}
NEXT:
	isAdmin := forum.GetUser(r).CanModerate()
	if topic.IsDeleted() && !isAdmin {
		http.Redirect(w, r, "/", 302)
		return
	}
	if len(topic.Posts) == 0 {
		http.Redirect(w, r, "/", 302)
		return
	}

	pages := int(math.Ceil(float64(len(topic.Posts)) / float64(server.PostsPerPage)))
	p, _ := strconv.Atoi(r.FormValue("p"))
	if p < 1 {
		p = 1
	}
	if p > pages {
		p = pages
	}

	topic.T_TotalPosts = uint16(len(topic.Posts) - 1)
	topic.T_IsAdmin = isAdmin
	posts := topic.Posts[(p-1)*server.PostsPerPage : int(math.Min(float64(p*server.PostsPerPage), float64(len(topic.Posts))))]
	if p == 1 {
		tmp := make([]server.Post, len(posts))
		copy(tmp, posts)
		topic.Posts = tmp
	} else {
		tmp := make([]server.Post, len(posts)+1)
		tmp[0] = topic.Posts[0]
		copy(tmp[1:], posts)
		topic.Posts = tmp
	}
	topic.Posts[0].T_IsFirst = true

	model := struct {
		server.Forum
		server.Topic
		NewPost
		Pages   int
		CurPage int
	}{
		Forum:   *forum,
		Topic:   topic,
		Pages:   pages,
		CurPage: p,
	}
	model.TopicID = topicID
	_, model.PostToken = forum.UUID()
	server.Render(w, server.TmplTopic, model)
}

func handleTopics(w http.ResponseWriter, r *http.Request) {
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
	isAdmin := forum.GetUser(r).CanModerate()
	topics, newFrom := forum.GetTopics(nTopicsMax, from, isAdmin)
	prevTo := from - nTopicsMax
	if prevTo < 0 {
		prevTo = -1
	}

	topicsDisplay := make([]server.Topic, 0)

	for _, t := range topics {
		if t.IsDeleted() && !isAdmin {
			continue
		}
		t.T_TotalPosts = uint16(len(t.Posts) - 1)
		t.T_IsAdmin = isAdmin
		t.T_IsExpand = true
		if len(t.Posts) > 5 {
			tmp := make([]server.Post, 5)
			tmp[0] = t.Posts[0]
			copy(tmp[1:], t.Posts[len(t.Posts)-4:])
			t.Posts = tmp
		} else {
			tmp := make([]server.Post, len(t.Posts))
			copy(tmp, t.Posts)
			t.Posts = tmp
		}
		t.Posts[0].T_IsFirst = true
		topicsDisplay = append(topicsDisplay, t)
	}

	model := struct {
		server.Forum
		NewPost
		NewFrom int
		PrevTo  int
		Topics  []server.Topic
	}{
		Forum:   *forum,
		Topics:  topicsDisplay,
		NewFrom: newFrom,
		PrevTo:  prevTo,
	}

	_, model.PostToken = forum.UUID()
	server.Render(w, server.TmplForum, model)
}

func handleRawPost(w http.ResponseWriter, r *http.Request) {
	longID, _ := strconv.ParseInt(r.URL.Path[len("/p/"):], 10, 64)
	topicID, postID := server.SplitID(uint64(longID))
	topic := forum.Store.TopicByID(topicID)
	if topic.ID == 0 {
		var err error
		topic, err = forum.LoadArchivedTopic(uint32(topicID))
		if err == nil {
			topic.Archived = true
			goto NEXT
		}
		w.WriteHeader(404)
		return
	}
NEXT:
	isAdmin := forum.GetUser(r).CanModerate()
	if topic.IsDeleted() && !isAdmin {
		w.WriteHeader(404)
		return
	}

	if int(postID) > len(topic.Posts) || postID == 0 {
		w.WriteHeader(404)
		return
	}

	topic.T_TotalPosts = uint16(len(topic.Posts) - 1)
	topic.T_IsExpand = true
	topic.Posts = topic.Posts[postID-1 : postID]

	model := struct {
		server.Forum
		server.Topic
		TopicID int
	}{
		Forum:   *forum,
		Topic:   topic,
		TopicID: int(topicID),
	}
	server.Render(w, server.TmplTopic1, model)
}
