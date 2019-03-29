// This code is in Public Domain. Take all the code you want, I'll just write more.
package handler

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/server"
)

func intmin(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

func intdivceil(a, b int) int {
	return int(math.Ceil(float64(a) / float64(b)))
}

type newPostInfo struct {
	TopicID   int
	PostToken string
}

// url: /t/{tid}
func Topic(w http.ResponseWriter, r *http.Request) {
	topicID, _ := strconv.Atoi(r.URL.Path[len("/t/"):])
	topic := common.Kforum.Store.GetTopic(uint32(topicID), server.DefaultTopicFilter)
	if topic.ID == 0 {
		var err error
		topic, err = common.Kforum.LoadArchivedTopic(uint32(topicID), common.Kforum.Salt)
		if err == nil {
			topic.Archived = true
			goto NEXT
		}

		common.Kforum.Notice("can't find topic with id %d, referer: %q, err: %v", topicID, r.Referer(), err)
		http.Redirect(w, r, "/", 302)
		return
	}
NEXT:
	isAdmin := common.Kforum.GetUser(r).CanModerate()
	if len(topic.Posts) == 0 {
		http.Redirect(w, r, "/", 302)
		return
	}

	pages := intdivceil(len(topic.Posts), common.Kforum.PostsPerPage)
	p, _ := strconv.Atoi(r.FormValue("p"))
	if p < 1 {
		p = 1
	}
	if p > pages {
		p = pages
	}

	topic.T_TotalPosts = uint16(len(topic.Posts) - 1)
	topic.T_IsAdmin = isAdmin
	posts := topic.Posts[(p-1)*common.Kforum.PostsPerPage : intmin(p*common.Kforum.PostsPerPage, len(topic.Posts))]
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
	topic.Posts[0].T_SetStatus(server.POST_ISFIRST)

	model := struct {
		server.Forum
		server.Topic
		newPostInfo
		Pages   int
		CurPage int
	}{
		Forum:   *common.Kforum,
		Topic:   topic,
		Pages:   pages,
		CurPage: p,
	}
	model.TopicID = topicID
	_, model.PostToken = common.Kforum.UUID()
	server.Render(w, server.TmplTopic, model)
}

func Topics(w http.ResponseWriter, r *http.Request) {
	p, _ := strconv.Atoi(r.FormValue("p"))
	if p < 1 {
		p = 1
	}

	isAdmin := common.Kforum.GetUser(r).CanModerate()
	topics := common.Kforum.GetTopics((p-1)*common.Kforum.TopicsPerPage, common.Kforum.TopicsPerPage, func(topic *server.Topic) server.Topic {
		t := *topic
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
		t.Posts[0].T_SetStatus(server.POST_ISFIRST)
		return t
	})

	model := struct {
		server.Forum
		newPostInfo
		Pages   int
		CurPage int
		Topics  []server.Topic
	}{
		Forum:   *common.Kforum,
		Topics:  topics,
		CurPage: p,
		Pages:   intdivceil(common.Kforum.LiveTopicsNum, common.Kforum.TopicsPerPage),
	}

	_, model.PostToken = common.Kforum.UUID()
	server.Render(w, server.TmplForum, model)
}

func Post(w http.ResponseWriter, r *http.Request) {
	longID, _ := strconv.ParseInt(r.URL.Path[len("/p/"):], 10, 64)
	topicID, postID := server.SplitID(uint64(longID))
	if r.FormValue("raw") != "1" {
		p := intdivceil(int(postID), common.Kforum.PostsPerPage)
		http.Redirect(w, r, fmt.Sprintf("/t/%d?p=%d#post-%d", topicID, p, longID), 302)
		return
	}

	topic := common.Kforum.Store.GetTopic(topicID, server.DefaultTopicFilter)
	if topic.ID == 0 {
		var err error
		topic, err = common.Kforum.LoadArchivedTopic(uint32(topicID), common.Kforum.Salt)
		if err == nil {
			topic.Archived = true
			goto NEXT
		}
		w.WriteHeader(404)
		return
	}
NEXT:
	if int(postID) > len(topic.Posts) || postID == 0 {
		w.WriteHeader(404)
		return
	}

	topic.T_TotalPosts = uint16(len(topic.Posts) - 1)
	topic.T_IsExpand = true
	topic.Posts = []server.Post{topic.Posts[postID-1]}
	topic.Posts[0].T_SetStatus(server.POST_ISREF)

	model := struct {
		server.Forum
		server.Topic
		TopicID int
	}{
		Forum:   *common.Kforum,
		Topic:   topic,
		TopicID: int(topicID),
	}
	server.Render(w, server.TmplTopic1, model)
}
