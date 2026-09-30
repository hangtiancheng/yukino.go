package router

import (
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/handler"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/middleware"

	"github.com/hangtiancheng/yukino.go/yukino_cache"
	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func Setup() *yukino_http.Application {
	app := yukino_http.Default()
	app.Use(middleware.CORS())
	app.Use(middleware.Auth())

	conf := config.Get()
	app.Static("/static/avatars", conf.Static.AvatarPath)
	app.Static("/static/files", conf.Static.FilePath)

	app.Post("/login", handler.Login)
	app.Post("/register", handler.Register)

	user := app.Router("/user")
	user.Post("/update-password", handler.UpdatePassword)
	user.Post("/search-user", handler.SearchUser)
	user.Post("/update-user-info", handler.UpdateUserInfo)
	user.Post("/get-user-info-list", middleware.RequireAdmin(handler.GetUserInfoList))
	user.Post("/able-users", middleware.RequireAdmin(handler.AbleUsers))
	user.Post("/get-user-info", handler.GetUserInfo)
	user.Post("/disable-users", middleware.RequireAdmin(handler.DisableUsers))
	user.Post("/delete-users", middleware.RequireAdmin(handler.DeleteUsers))
	user.Post("/set-admin", middleware.RequireAdmin(handler.SetAdmin))
	user.Post("/ws-logout", handler.WsLogout)

	group := app.Router("/group")
	group.Post("/create-group", handler.CreateGroup)
	group.Post("/load-my-group", handler.LoadMyGroup)
	group.Post("/check-group-add-mode", handler.CheckGroupAddMode)
	group.Post("/enter-group-directly", handler.EnterGroupDirectly)
	group.Post("/leave-group", handler.LeaveGroup)
	group.Post("/dismiss-group", handler.DismissGroup)
	group.Post("/get-group-info", handler.GetGroupInfo)
	group.Post("/update-group-info", handler.UpdateGroupInfo)
	group.Post("/get-group-member-list", handler.GetGroupMemberList)
	group.Post("/remove-group-members", handler.RemoveGroupMembers)
	group.Post("/invite-group-members", handler.InviteGroupMembers)
	group.Post("/search-group", handler.SearchGroup)
	group.Post("/get-group-info-list", middleware.RequireAdmin(handler.GetGroupInfoList))
	group.Post("/delete-groups", middleware.RequireAdmin(handler.DeleteGroups))
	group.Post("/set-groups-status", middleware.RequireAdmin(handler.SetGroupsStatus))

	session := app.Router("/session")
	session.Post("/open-session", handler.OpenSession)
	session.Post("/get-user-session-list", handler.GetUserSessionList)
	session.Post("/get-group-session-list", handler.GetGroupSessionList)
	session.Post("/delete-session", handler.DeleteSession)
	session.Post("/check-open-session-allowed", handler.CheckOpenSessionAllowed)
	session.Post("/mark-session-read", handler.MarkSessionRead)

	contact := app.Router("/contact")
	contact.Post("/get-user-list", handler.GetUserList)
	contact.Post("/get-tag-list", handler.GetTagList)
	contact.Post("/add-tag", handler.AddTag)
	contact.Post("/update-contact", handler.UpdateContact)
	contact.Post("/load-my-joined-group", handler.LoadMyJoinedGroup)
	contact.Post("/get-contact-info", handler.GetContactInfo)
	contact.Post("/apply-contact", handler.ApplyContact)
	contact.Post("/get-new-contact-list", handler.GetNewContactList)
	contact.Post("/pass-contact-apply", handler.PassContactApply)
	contact.Post("/refuse-contact-apply", handler.RefuseContactApply)
	contact.Post("/black-contact", handler.BlackContact)
	contact.Post("/cancel-black-contact", handler.CancelBlackContact)
	contact.Post("/black-apply", handler.BlackApply)
	contact.Post("/get-add-group-list", handler.GetAddGroupList)
	contact.Post("/delete-contact", handler.DeleteContact)

	message := app.Router("/message")
	message.Post("/get-message-list", handler.GetMessageList)
	message.Post("/get-group-message-list", handler.GetGroupMessageList)
	message.Post("/upload-avatar", handler.UploadAvatar)
	message.Post("/upload-file", handler.UploadFile)

	file := app.Router("/file")
	file.Post("/verify", handler.VerifyFile)
	file.Post("/upload-chunk", handler.UploadChunk)
	file.Post("/merge", handler.MergeFile)

	chatroom := app.Router("/chatroom")
	chatroom.Post("/get-online-users", handler.GetOnlineUsers)
	chatroom.Post("/get-callers", handler.GetCallers)

	app.Get("/wss", handler.WsLogin)
	app.Get("/dashboard/ws", yukino_cache.DashboardHandler())

	return app
}
