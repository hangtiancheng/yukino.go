package service

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// CreationApp is the creation application-service facade: it aggregates the four use cases for interfaces/monitor to call.
type CreationApp struct {
	PreCreate   *PreCreateApp
	Create      *CreateApp
	PostCreate  *PostCreateApp
	HandleError *HandleErrorApp
}

// NewCreationApp creates the creation facade (the four use cases share one set of repositories and cross-domain ports).
func NewCreationApp(projects domain.ProjectRepository, records domain.CallbackRecordRepository, risk RiskPort, points PointsPort) *CreationApp {
	return &CreationApp{
		PreCreate:   NewPreCreateApp(projects, risk, points),
		Create:      NewCreateApp(projects, points),
		PostCreate:  NewPostCreateApp(projects, records, risk, points),
		HandleError: NewHandleErrorApp(projects, points),
	}
}
