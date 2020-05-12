// Code generated by Wire. DO NOT EDIT.

//go:generate wire
//+build !wireinject

package router

import (
	"github.com/leandro-lugaresi/hub"
	"github.com/traPtitech/traQ/rbac"
	"github.com/traPtitech/traQ/repository"
	"github.com/traPtitech/traQ/router/oauth2"
	"github.com/traPtitech/traQ/router/v1"
	"github.com/traPtitech/traQ/router/v3"
	"github.com/traPtitech/traQ/service"
	"go.uber.org/zap"
)

// Injectors from router_wire.go:

func newRouter(hub2 *hub.Hub, repo repository.Repository, ss *service.Services, rbac2 rbac.RBAC, logger *zap.Logger, config *Config) *Router {
	echo := newEcho(logger, config, repo)
	streamer := ss.SSE
	onlineCounter := ss.OnlineCounter
	manager := ss.ViewerManager
	heartbeatManager := ss.HeartBeats
	processor := ss.Imaging
	handlers := &v1.Handlers{
		RBAC:       rbac2,
		Repo:       repo,
		SSE:        streamer,
		Hub:        hub2,
		Logger:     logger,
		OC:         onlineCounter,
		VM:         manager,
		HeartBeats: heartbeatManager,
		Imaging:    processor,
	}
	wsStreamer := ss.WS
	webrtcv3Manager := ss.WebRTCv3
	v3Config := provideV3Config(config)
	v3Handlers := &v3.Handlers{
		RBAC:    rbac2,
		Repo:    repo,
		WS:      wsStreamer,
		Hub:     hub2,
		Logger:  logger,
		OC:      onlineCounter,
		VM:      manager,
		WebRTC:  webrtcv3Manager,
		Imaging: processor,
		Config:  v3Config,
	}
	oauth2Config := provideOAuth2Config(config)
	handler := &oauth2.Handler{
		RBAC:   rbac2,
		Repo:   repo,
		Logger: logger,
		Config: oauth2Config,
	}
	router := &Router{
		e:      echo,
		v1:     handlers,
		v3:     v3Handlers,
		oauth2: handler,
	}
	return router
}
