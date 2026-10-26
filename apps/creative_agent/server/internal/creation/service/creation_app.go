package service

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// CreationApp 创作应用服务门面：聚合四个用例，供 interfaces/monitor 调用。
type CreationApp struct {
	PreCreate   *PreCreateApp
	Create      *CreateApp
	PostCreate  *PostCreateApp
	HandleError *HandleErrorApp
}

// NewCreationApp 创建创作门面（四个用例共享同一组仓储与跨域端口）。
func NewCreationApp(projects domain.ProjectRepository, records domain.CallbackRecordRepository, risk RiskPort, points PointsPort) *CreationApp {
	return &CreationApp{
		PreCreate:   NewPreCreateApp(projects, risk, points),
		Create:      NewCreateApp(projects, points),
		PostCreate:  NewPostCreateApp(projects, records, risk, points),
		HandleError: NewHandleErrorApp(projects, points),
	}
}
