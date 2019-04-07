package handler

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/coyove/fofou/common"
	"github.com/coyove/fofou/server"
)

func modCode(forum *server.Forum, u server.User, subject, msg string) bool {
	r := bufio.NewReader(strings.NewReader(msg))
	opcode := false

	if u.Can(server.PERM_APPEND_ANNOUNCE) {
		if strings.HasPrefix(subject, "!!append=") {
			vint, _ := strconv.ParseInt(subject[9:], 10, 64)
			common.Kforum.AppendPost(uint64(vint), "\n"+msg)
			return true
		}
		if strings.HasPrefix(subject, "!!announce") {
			common.Kforum.ForumConfig.Announcement = msg
			opcode = true
			goto UPDATE
		}
	}

	for {
		line, _, err := r.ReadLine()
		if err != nil {
			break
		}

		msg := string(line)
		if !strings.HasPrefix(msg, "!!") {
			break
		}

		eidx := strings.Index(msg, "=")
		if eidx == -1 {
			break
		}

		v := msg[eidx+1:]
		vint, _ := strconv.ParseInt(v, 10, 64)
		switch op := msg[2:eidx]; op {
		case "moat":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			switch v {
			case "cookie":
				common.Kforum.NoMoreNewUsers = !common.Kforum.NoMoreNewUsers
			case "image":
				common.Kforum.NoImageUpload = !common.Kforum.NoImageUpload
			case "recaptcha":
				common.Kforum.NoRecaptcha = !common.Kforum.NoRecaptcha
			case "production":
				common.Kprod = !common.Kprod
				common.Kforum.Logger.UseStdout = !common.Kprod
			}
			opcode = true
		case "max-message-len":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.MaxMessageLen = int(vint)
			opcode = true
		case "max-subject-len":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.MaxSubjectLen = int(vint)
			opcode = true
		case "search-timeout":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.SearchTimeout = int(vint)
			opcode = true
		case "cooldown":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.Cooldown = int(vint)
			opcode = true
		case "max-image-size":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.MaxImageSize = int(vint)
			opcode = true
		case "delete", "delete-image":
			res := common.Kforum.Store.DeletePost(u, uint64(vint), op == "delete-image", func(img *server.Image) {
				if img != nil {
					os.Remove(common.DATA_IMAGES + img.Path)
					os.Remove(common.DATA_IMAGES + img.Path + ".thumb.jpg")
				}
			})
			opcode = true
			if res != nil {
				break
			}
		case "stick":
			if !u.Can(server.PERM_STICKY_PURGE) {
				return true
			}
			common.Kforum.Store.OperateTopic(uint32(vint), server.OP_STICKY)
			opcode = true
		case "lock":
			if !u.Can(server.PERM_LOCK_SAGE_DELETE) {
				return true
			}
			common.Kforum.Store.OperateTopic(uint32(vint), server.OP_LOCK)
			opcode = true
		case "purge":
			if !u.Can(server.PERM_STICKY_PURGE) {
				return true
			}
			common.Kforum.Store.OperateTopic(uint32(vint), server.OP_PURGE)
			opcode = true
		case "free-reply":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.Store.OperateTopic(uint32(vint), server.OP_FREEREPLY)
			opcode = true
		case "sage":
			opcode = true
			if !u.Can(server.PERM_LOCK_SAGE_DELETE) && (u.Posts < u.N) {
				// special case: normal users can't sage their threads until certain criterias met
				return true
			}
			res := common.Kforum.Store.SageTopic(uint32(vint), u)
			if res != nil {
				break
			}
		case "block":
			if !u.Can(server.PERM_BLOCK) {
				return true
			}
			common.Kforum.Store.Block(server.Parse8Bytes(v))
			opcode = true
		case "title":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.Title = v
			opcode = true
		case "max-live-topics":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.SetMaxLiveTopics(int(vint))
			opcode = true
		case "url":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			common.Kforum.URL = v
			opcode = true
		}
	}

UPDATE:
	if opcode {
		err := forum.UpdateConfig(common.Kforum.ForumConfig)
		if err != nil {
			forum.Logger.Error("update config: %v", err)
		}
	}

	return opcode
}
