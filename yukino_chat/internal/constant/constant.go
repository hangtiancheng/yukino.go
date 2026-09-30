package constant

const (
	ChannelSize = 1024
	SystemError = "Internal Server Error"
)

// UserStatus
const (
	UserStatusNormal  int8 = 0
	UserStatusDisable int8 = 1
)

// MessageStatus
const (
	MessageUnsent int8 = 0
	MessageSent   int8 = 1
)

// MessageType
const (
	MessageText         int8 = 0
	MessageImage        int8 = 1
	MessageFile         int8 = 2
	MessageAudioOrVideo int8 = 3
	MessageVideo        int8 = 4
	MessageSystem       int8 = 5
)

// System notification topics carried in the content field of MessageSystem
// frames. Clients re-fetch the matching list when one arrives.
const (
	NotifyContact = "contact"
	NotifyGroup   = "group"
	NotifyApply   = "apply"
	NotifySession = "session"
	NotifyOnline  = "online"
)

// ContactStatus
const (
	ContactNormal   int8 = 0
	ContactBlack    int8 = 1
	ContactBeBlack  int8 = 2
	ContactDelete   int8 = 3
	ContactBeDelete int8 = 4
	ContactMute     int8 = 5
	ContactQuit     int8 = 6
	ContactKicked   int8 = 7
)

// ContactType
const (
	ContactTypeUser  int8 = 0
	ContactTypeGroup int8 = 1
)

// ContactApplyStatus
const (
	ApplyStatusApplying int8 = 0
	ApplyStatusPass     int8 = 1
	ApplyStatusRefuse   int8 = 2
	ApplyStatusBlack    int8 = 3
)

// GroupAddMode
const (
	GroupAddModeDirect int8 = 0
	GroupAddModeReview int8 = 1
)

// GroupStatus
const (
	GroupStatusNormal  int8 = 0
	GroupStatusDisable int8 = 1
	GroupStatusDismiss int8 = 2
)
