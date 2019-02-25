// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/common/rand"
)

// ModelNewPost represents a new post
type ModelNewPost struct {
	*Forum
	TopicID        int
	PrevCaptcha    string
	PrevSubject    string
	Token          string
	SubjectError   bool
	MessageError   bool
	TokenError     bool
	TopicLocked    bool
	NoMoreNewUsers bool
	PrevMessage    string
	NameClass      string
	PrevName       string
}

var randG = rand.New()

func getIPAddress(r *http.Request) (v [8]byte) {
	ipAddr := ""
	hdrRealIP, hdrForwardedFor := r.Header.Get("X-Real-Ip"), r.Header.Get("X-Forwarded-For")

	if hdrRealIP == "" && hdrForwardedFor == "" {
		s := r.RemoteAddr
		idx := strings.LastIndex(s, ":")
		if idx == -1 {
			ipAddr = s
		} else {
			ipAddr = s[:idx]
		}
	} else if hdrForwardedFor != "" {
		parts := strings.Split(hdrForwardedFor, ",")
		ipAddr = strings.TrimSpace(parts[0])
	} else {
		ipAddr = hdrRealIP
	}

	ip := net.ParseIP(ipAddr)
	if len(ip) == 0 {
		return
	}
	ipv4 := ip.To4()
	if len(ipv4) == 0 {
		copy(v[:], ip)
		return
	}
	copy(v[4:], ipv4[:3])
	return
}

func writeSimpleJSON(w http.ResponseWriter, args ...interface{}) {
	var p bytes.Buffer
	p.WriteString("{")
	for i := 0; i < len(args); i += 2 {
		k, _ := args[i].(string)
		p.WriteByte('"')
		p.WriteString(k)
		p.WriteString(`":`)
		buf, _ := json.Marshal(args[i+1])
		p.Write(buf)
		p.WriteByte(',')
	}
	if len(args) > 0 {
		p.Truncate(p.Len() - 1)
	}
	p.WriteString("}")
	w.Write(p.Bytes())
}

func createNewPost(forum *Forum, topic *Topic, w http.ResponseWriter, r *http.Request) {
	badRequest := func() { writeSimpleJSON(w, "success", false, "error", "bad-request") }
	internalError := func() { writeSimpleJSON(w, "success", false, "error", "internal-error") }

	ipAddr := getIPAddress(r)
	if forum.Store.IsBlocked("b" + IPAddress(ipAddr)) {
		logger.Noticef("blocked a post from IP %s", IPAddress(ipAddr))
		badRequest()
		return
	}

	recaptcha := strings.TrimSpace(r.FormValue("g-recaptcha-response"))
	if recaptcha == "" {
		badRequest()
		return
	}

	resp, err := (&http.Client{Timeout: time.Second * 5}).PostForm("https://www.recaptcha.net/recaptcha/api/siteverify", url.Values{
		"secret":   []string{forum.Recaptcha},
		"response": []string{recaptcha},
	})
	if err != nil {
		logger.Errorf("recaptcha error: %v", err)
		internalError()
		return
	}

	defer resp.Body.Close()
	buf, _ := ioutil.ReadAll(resp.Body)

	recaptchaResult := map[string]interface{}{}
	json.Unmarshal(buf, &recaptchaResult)

	if r, _ := recaptchaResult["success"].(bool); !r {
		logger.Errorf("recaptcha failed: %v", string(buf))
		writeSimpleJSON(w, "success", false, "error", "recaptcha-failed")
		return
	}

	subject := strings.Replace(r.FormValue("Subject"), "<", "&lt;", -1)
	msg := strings.TrimSpace(r.FormValue("Message"))
	userName := getSecureCookie(r)

	// validate the fields
	if userName == "" {
		if forum.NoMoreNewUsers && !topic.FreeReply {
			writeSimpleJSON(w, "success", false, "error", "no-more-new-users")
			return
		}
		userName = base64.URLEncoding.EncodeToString(randG.Fetch(6))
	}

	if forum.IsAdmin(userName) && adminOpCode(forum, msg) {
		writeSimpleJSON(w, "success", true, "admin-operation", msg)
		return
	}

	if topic == nil {
		if tmp := []rune(subject); len(tmp) > forum.MaxSubjectLen {
			tmp[forum.MaxSubjectLen-1], tmp[forum.MaxSubjectLen-2], tmp[forum.MaxSubjectLen-3] = '.', '.', '.'
			subject = string(tmp[:forum.MaxSubjectLen])
		}
	}

	if len(msg) > forum.MaxMessageLen {
		// hard trunc
		msg = msg[:forum.MaxMessageLen]
	}

	if len(msg) < forum.MinMessageLen {
		writeSimpleJSON(w, "success", false, "error", "message-too-short")
		return
	}

	if topic != nil && topic.Locked {
		writeSimpleJSON(w, "success", false, "error", "topic-locked")
		return
	}

	setSecureCookie(w, userName)

	if forum.Store.IsBlocked("u" + userName) {
		logger.Noticef("blocked a post from user %s", userName)
		badRequest()
		return
	}

	if topic == nil {
		topicID, err := forum.Store.CreateNewTopic(subject, msg, userName, ipAddr)
		if err != nil {
			logger.Errorf("createNewPost(): store.CreateNewPost() failed with %s", err)
			internalError()
			return
		}
		writeSimpleJSON(w, "success", true, "topic", topicID)
	}

	if err := forum.Store.AddPostToTopic(topic.ID, msg, userName, ipAddr); err != nil {
		logger.Errorf("createNewPost(): store.AddPostToTopic() failed with %s", err)
		internalError()
		return
	}
	writeSimpleJSON(w, "success", true, "topic", topic.ID)
}

// url: /{forum}/newpost[?topicId={topicId}]
func handleNewPost(forum *Forum, w http.ResponseWriter, r *http.Request) {
	var topic *Topic
	topicID := 0

	if topicIDStr := strings.TrimSpace(r.FormValue("tid")); topicIDStr != "" {
		topicID, _ = strconv.Atoi(topicIDStr)
		if topic = forum.Store.TopicByID(uint32(topicID)); topic == nil {
			logger.Noticef("handleNewPost(): invalid topicId: %d\n", topicID)
			http.Redirect(w, r, fmt.Sprintf("/%s", forum.ForumUrl), 302)
			return
		}
	}

	//fmt.Printf("handleNewPost(): forum: %q, topicId: %d\n", forum.ForumUrl, topicId)
	cookie := getSecureCookie(r)
	model := &ModelNewPost{
		Forum:    forum,
		TopicID:  topicID,
		PrevName: cookie,
	}

	if topic != nil {
		model.TopicLocked = topic.Locked
	}

	if r.Method == "POST" {
		createNewPost(forum, topic, w, r)
		return
	}

	if topicID != 0 {
		model.PrevSubject = topic.Subject
	}

	ExecTemplate(w, tmplNewPost, model)
}
