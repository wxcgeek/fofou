// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"net/http"
	"time"

	atom "github.com/kjk/atomgenerator"
)

//func buildForumURL(r *http.Request, forum *Forum) string {
//	return fmt.Sprintf("http://%s/", r.Host)
//}
//
//func buildTopicURL(r *http.Request, forum *Forum, p *Post) string {
//	return fmt.Sprintf("http://%s/topic?id=%d&post=%d", r.Host, p.Topic.ID, p.ID)
//}
//
//func buildTopicID(r *http.Request, forum *Forum, p *Post) string {
//	pubDateStr := time.Unix(int64(p.CreatedAt), 0).Format("2006-01-02")
//	url := fmt.Sprintf("/%s/topic?id=%d&post=%d", forum.ForumUrl, p.Topic.ID, p.ID)
//	return fmt.Sprintf("tag:%s,%s:%s", r.Host, pubDateStr, url)
//}

func handleRSS(forum *Forum, w http.ResponseWriter, r *http.Request) {
	topics, _ := forum.Store.GetTopics(25, 0, false)
	posts := make([]*Post, len(topics), len(topics))
	for i, t := range topics {
		posts[i] = &t.Posts[0]
	}

	pubTime := time.Now()
	if len(posts) > 0 {
		pubTime = time.Unix(int64(posts[0].CreatedAt), 0)
	}

	feed := &atom.Feed{
		Title: forum.Title,
		//Link:    buildForumURL(r, forum),
		PubDate: pubTime,
	}

	for _, p := range posts {
		//id := fmt.Sprintf("tag:forums.fofou.org,1999:%s-topic-%d-post-%d", forum.ForumUrl, p.Topic.Id, p.Id)
		e := &atom.Entry{
			//Id:      buildTopicID(r, forum, p),
			Title:   p.Topic.Subject,
			PubDate: time.Unix(int64(p.CreatedAt), 0),
			//Link:    buildTopicURL(r, forum, p),
			Content: p.MessageHTML(),
		}
		feed.AddEntry(e)
	}

	s, err := feed.GenXml()
	if err != nil {
		s = []byte("Failed to generate XML feed")
	}

	w.Write(s)
}
