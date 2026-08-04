package routes

import (
	"github.com/gin-gonic/gin"
	managementhandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

// registerManagementDataSourceRoutes registers the TokenHub platform data
// collection endpoints (member list + usage events).
func registerManagementDataSourceRoutes(group *gin.RouterGroup, h *managementhandlers.Handler) {
	group.GET("/data-source/members", h.GetDataSourceMembers)
	group.GET("/data-source/usage-events", h.GetDataSourceUsageEvents)
}
