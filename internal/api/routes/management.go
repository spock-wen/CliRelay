package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	managementhandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

type ManagementOptions struct {
	Availability       gin.HandlerFunc
	PublicNoStore      gin.HandlerFunc
	PublicRateLimit    gin.HandlerFunc
	ClearWriteDeadline func(*gin.Context)
}

func RegisterManagement(engine *gin.Engine, h *managementhandlers.Handler, opts ManagementOptions) {
	if engine == nil || h == nil {
		return
	}

	clearWriteDeadline := opts.ClearWriteDeadline
	if clearWriteDeadline == nil {
		clearWriteDeadline = func(*gin.Context) {}
	}

	mgmtMiddlewares := make([]gin.HandlerFunc, 0, 3)
	if opts.Availability != nil {
		mgmtMiddlewares = append(mgmtMiddlewares, opts.Availability)
	}
	mgmtMiddlewares = append(mgmtMiddlewares, h.Middleware(), bodyutil.LimitBodyMiddleware(bodyutil.ManagementBodyLimit))

	mgmt := engine.Group("/v0/management")
	mgmt.Use(mgmtMiddlewares...)
	{
		mgmt.GET("/dashboard-summary", h.GetDashboardSummary)
		mgmt.GET("/system-stats", h.GetSystemStats)
		mgmt.GET("/system-stats/ws", func(c *gin.Context) {
			clearWriteDeadline(c)
			h.SystemStatsWebSocket(c)
		})
		mgmt.GET("/models", h.GetModels)
		mgmt.GET("/models/configured-availability", h.GetConfiguredModelAvailability)
		mgmt.GET("/model-path-availability", h.GetModelPathAvailability)
		mgmt.GET("/model-configs", h.GetModelConfigs)
		mgmt.POST("/model-configs", h.PostModelConfig)
		mgmt.PUT("/model-configs/*id", h.PutModelConfig)
		mgmt.DELETE("/model-configs/*id", h.DeleteModelConfig)
		mgmt.GET("/model-owner-presets", h.GetModelOwnerPresets)
		mgmt.PUT("/model-owner-presets", h.PutModelOwnerPresets)
		mgmt.GET("/model-openrouter-sync", h.GetOpenRouterModelSync)
		mgmt.PUT("/model-openrouter-sync", h.PutOpenRouterModelSync)
		mgmt.POST("/model-openrouter-sync/run", h.PostOpenRouterModelSyncRun)
		mgmt.GET("/channel-groups", h.GetChannelGroups)
		mgmt.GET("/ccswitch-import-configs", h.GetCcSwitchImportConfigs)
		mgmt.PUT("/ccswitch-import-configs", h.PutCcSwitchImportConfigs)
		mgmt.GET("/routing-config", h.GetRoutingConfig)
		mgmt.PUT("/routing-config", h.PutRoutingConfig)
		mgmt.GET("/identity-fingerprint", h.GetIdentityFingerprint)
		mgmt.PUT("/identity-fingerprint", h.PutIdentityFingerprint)
		mgmt.GET("/model-pricing", h.GetModelPricing)
		mgmt.PUT("/model-pricing", h.PutModelPricing)
		mgmt.GET("/usage", h.GetUsageStatistics)
		mgmt.GET("/usage/export", h.ExportUsageStatistics)
		mgmt.POST("/usage/import", h.ImportUsageStatistics)
		mgmt.GET("/usage/logs", h.GetUsageLogs)
		mgmt.DELETE("/usage/logs", h.DeleteUsageLogs)
		mgmt.GET("/usage/logs/:id/content", h.GetLogContent)
		mgmt.GET("/usage/auth-file-group-trend", h.GetAuthFileGroupTrend)
		mgmt.GET("/usage/auth-file-trend", h.GetAuthFileTrend)
		mgmt.POST("/usage/auth-file-quota-snapshot", h.PostAuthFileQuotaSnapshot)
		mgmt.GET("/usage/chart-data", h.GetUsageChartData)
		mgmt.GET("/usage/entity-stats", h.GetEntityUsageStats)
		mgmt.GET("/config", h.GetConfig)
		mgmt.GET("/config.yaml", h.GetConfigYAML)
		mgmt.PUT("/config.yaml", h.PutConfigYAML)
		mgmt.GET("/latest-version", h.GetLatestVersion)
		mgmt.GET("/update/check", h.CheckUpdate)
		mgmt.GET("/update/current", h.GetCurrentUpdateState)
		mgmt.GET("/update/progress", h.GetUpdateProgress)
		mgmt.POST("/update/apply", h.ApplyUpdate)
		mgmt.GET("/auto-update/enabled", h.GetAutoUpdateEnabled)
		mgmt.PUT("/auto-update/enabled", h.PutAutoUpdateEnabled)
		mgmt.PATCH("/auto-update/enabled", h.PutAutoUpdateEnabled)
		mgmt.GET("/auto-update/channel", h.GetAutoUpdateChannel)
		mgmt.PUT("/auto-update/channel", h.PutAutoUpdateChannel)
		mgmt.PATCH("/auto-update/channel", h.PutAutoUpdateChannel)

		mgmt.GET("/debug", h.GetDebug)
		mgmt.PUT("/debug", h.PutDebug)
		mgmt.PATCH("/debug", h.PutDebug)

		mgmt.GET("/logging-to-file", h.GetLoggingToFile)
		mgmt.PUT("/logging-to-file", h.PutLoggingToFile)
		mgmt.PATCH("/logging-to-file", h.PutLoggingToFile)

		mgmt.GET("/logs-max-total-size-mb", h.GetLogsMaxTotalSizeMB)
		mgmt.PUT("/logs-max-total-size-mb", h.PutLogsMaxTotalSizeMB)
		mgmt.PATCH("/logs-max-total-size-mb", h.PutLogsMaxTotalSizeMB)

		mgmt.GET("/error-logs-max-files", h.GetErrorLogsMaxFiles)
		mgmt.PUT("/error-logs-max-files", h.PutErrorLogsMaxFiles)
		mgmt.PATCH("/error-logs-max-files", h.PutErrorLogsMaxFiles)

		mgmt.GET("/usage-statistics-enabled", h.GetUsageStatisticsEnabled)
		mgmt.PUT("/usage-statistics-enabled", h.PutUsageStatisticsEnabled)
		mgmt.PATCH("/usage-statistics-enabled", h.PutUsageStatisticsEnabled)

		mgmt.GET("/proxy-url", h.GetProxyURL)
		mgmt.PUT("/proxy-url", h.PutProxyURL)
		mgmt.PATCH("/proxy-url", h.PutProxyURL)
		mgmt.DELETE("/proxy-url", h.DeleteProxyURL)
		mgmt.GET("/proxy-pool", h.GetProxyPool)
		mgmt.PUT("/proxy-pool", h.PutProxyPool)
		mgmt.POST("/proxy-pool/check", h.PostProxyPoolCheck)

		mgmt.POST("/api-call", h.APICall)

		mgmt.GET("/quota-exceeded/switch-project", h.GetSwitchProject)
		mgmt.PUT("/quota-exceeded/switch-project", h.PutSwitchProject)
		mgmt.PATCH("/quota-exceeded/switch-project", h.PutSwitchProject)

		mgmt.GET("/quota-exceeded/switch-preview-model", h.GetSwitchPreviewModel)
		mgmt.PUT("/quota-exceeded/switch-preview-model", h.PutSwitchPreviewModel)
		mgmt.PATCH("/quota-exceeded/switch-preview-model", h.PutSwitchPreviewModel)
		mgmt.POST("/quota/reconcile", h.PostQuotaReconcile)

		mgmt.GET("/api-keys", h.GetAPIKeys)
		mgmt.PUT("/api-keys", h.PutAPIKeys)
		mgmt.PATCH("/api-keys", h.PatchAPIKeys)
		mgmt.DELETE("/api-keys", h.DeleteAPIKeys)

		mgmt.GET("/api-key-permission-profiles", h.GetAPIKeyPermissionProfiles)
		mgmt.PUT("/api-key-permission-profiles", h.PutAPIKeyPermissionProfiles)

		mgmt.GET("/api-key-entries", h.GetAPIKeyEntries)
		mgmt.PUT("/api-key-entries", h.PutAPIKeyEntries)
		mgmt.PATCH("/api-key-entries", h.PatchAPIKeyEntry)
		mgmt.DELETE("/api-key-entries", h.DeleteAPIKeyEntry)

		mgmt.GET("/gemini-api-key", h.GetGeminiKeys)
		mgmt.PUT("/gemini-api-key", h.PutGeminiKeys)
		mgmt.PATCH("/gemini-api-key", h.PatchGeminiKey)
		mgmt.DELETE("/gemini-api-key", h.DeleteGeminiKey)

		mgmt.GET("/logs", h.GetLogs)
		mgmt.DELETE("/logs", h.DeleteLogs)
		mgmt.GET("/request-error-logs", h.GetRequestErrorLogs)
		mgmt.GET("/request-error-logs/:name", h.DownloadRequestErrorLog)
		mgmt.GET("/request-log-by-id/:id", h.GetRequestLogByID)
		mgmt.GET("/request-log", h.GetRequestLog)
		mgmt.PUT("/request-log", h.PutRequestLog)
		mgmt.PATCH("/request-log", h.PutRequestLog)
		mgmt.GET("/ws-auth", h.GetWebsocketAuth)
		mgmt.PUT("/ws-auth", h.PutWebsocketAuth)
		mgmt.PATCH("/ws-auth", h.PutWebsocketAuth)

		mgmt.GET("/ampcode", h.GetAmpCode)
		mgmt.GET("/ampcode/upstream-url", h.GetAmpUpstreamURL)
		mgmt.PUT("/ampcode/upstream-url", h.PutAmpUpstreamURL)
		mgmt.PATCH("/ampcode/upstream-url", h.PutAmpUpstreamURL)
		mgmt.DELETE("/ampcode/upstream-url", h.DeleteAmpUpstreamURL)
		mgmt.GET("/ampcode/upstream-api-key", h.GetAmpUpstreamAPIKey)
		mgmt.PUT("/ampcode/upstream-api-key", h.PutAmpUpstreamAPIKey)
		mgmt.PATCH("/ampcode/upstream-api-key", h.PutAmpUpstreamAPIKey)
		mgmt.DELETE("/ampcode/upstream-api-key", h.DeleteAmpUpstreamAPIKey)
		mgmt.GET("/ampcode/restrict-management-to-localhost", h.GetAmpRestrictManagementToLocalhost)
		mgmt.PUT("/ampcode/restrict-management-to-localhost", h.PutAmpRestrictManagementToLocalhost)
		mgmt.PATCH("/ampcode/restrict-management-to-localhost", h.PutAmpRestrictManagementToLocalhost)
		mgmt.GET("/ampcode/model-mappings", h.GetAmpModelMappings)
		mgmt.PUT("/ampcode/model-mappings", h.PutAmpModelMappings)
		mgmt.PATCH("/ampcode/model-mappings", h.PatchAmpModelMappings)
		mgmt.DELETE("/ampcode/model-mappings", h.DeleteAmpModelMappings)
		mgmt.GET("/ampcode/force-model-mappings", h.GetAmpForceModelMappings)
		mgmt.PUT("/ampcode/force-model-mappings", h.PutAmpForceModelMappings)
		mgmt.PATCH("/ampcode/force-model-mappings", h.PutAmpForceModelMappings)
		mgmt.GET("/ampcode/upstream-api-keys", h.GetAmpUpstreamAPIKeys)
		mgmt.PUT("/ampcode/upstream-api-keys", h.PutAmpUpstreamAPIKeys)
		mgmt.PATCH("/ampcode/upstream-api-keys", h.PatchAmpUpstreamAPIKeys)
		mgmt.DELETE("/ampcode/upstream-api-keys", h.DeleteAmpUpstreamAPIKeys)

		mgmt.GET("/request-retry", h.GetRequestRetry)
		mgmt.PUT("/request-retry", h.PutRequestRetry)
		mgmt.PATCH("/request-retry", h.PutRequestRetry)
		mgmt.GET("/max-retry-interval", h.GetMaxRetryInterval)
		mgmt.PUT("/max-retry-interval", h.PutMaxRetryInterval)
		mgmt.PATCH("/max-retry-interval", h.PutMaxRetryInterval)

		mgmt.GET("/force-model-prefix", h.GetForceModelPrefix)
		mgmt.PUT("/force-model-prefix", h.PutForceModelPrefix)
		mgmt.PATCH("/force-model-prefix", h.PutForceModelPrefix)

		mgmt.GET("/routing/strategy", h.GetRoutingStrategy)
		mgmt.PUT("/routing/strategy", h.PutRoutingStrategy)
		mgmt.PATCH("/routing/strategy", h.PutRoutingStrategy)

		mgmt.GET("/claude-api-key", h.GetClaudeKeys)
		mgmt.PUT("/claude-api-key", h.PutClaudeKeys)
		mgmt.PATCH("/claude-api-key", h.PatchClaudeKey)
		mgmt.DELETE("/claude-api-key", h.DeleteClaudeKey)

		mgmt.GET("/bedrock-api-key", h.GetBedrockKeys)
		mgmt.PUT("/bedrock-api-key", h.PutBedrockKeys)
		mgmt.PATCH("/bedrock-api-key", h.PatchBedrockKey)
		mgmt.DELETE("/bedrock-api-key", h.DeleteBedrockKey)

		mgmt.GET("/opencode-go-api-key", h.GetOpenCodeGoKeys)
		mgmt.PUT("/opencode-go-api-key", h.PutOpenCodeGoKeys)
		mgmt.PATCH("/opencode-go-api-key", h.PatchOpenCodeGoKey)
		mgmt.DELETE("/opencode-go-api-key", h.DeleteOpenCodeGoKey)
		mgmt.POST("/opencode-go-api-key/usage", h.QueryOpenCodeGoUsage)

		mgmt.GET("/codex-api-key", h.GetCodexKeys)
		mgmt.PUT("/codex-api-key", h.PutCodexKeys)
		mgmt.PATCH("/codex-api-key", h.PatchCodexKey)
		mgmt.DELETE("/codex-api-key", h.DeleteCodexKey)

		mgmt.GET("/openai-compatibility", h.GetOpenAICompat)
		mgmt.PUT("/openai-compatibility", h.PutOpenAICompat)
		mgmt.PATCH("/openai-compatibility", h.PatchOpenAICompat)
		mgmt.DELETE("/openai-compatibility", h.DeleteOpenAICompat)

		mgmt.GET("/vertex-api-key", h.GetVertexCompatKeys)
		mgmt.PUT("/vertex-api-key", h.PutVertexCompatKeys)
		mgmt.PATCH("/vertex-api-key", h.PatchVertexCompatKey)
		mgmt.DELETE("/vertex-api-key", h.DeleteVertexCompatKey)

		mgmt.GET("/oauth-excluded-models", h.GetOAuthExcludedModels)
		mgmt.PUT("/oauth-excluded-models", h.PutOAuthExcludedModels)
		mgmt.PATCH("/oauth-excluded-models", h.PatchOAuthExcludedModels)
		mgmt.DELETE("/oauth-excluded-models", h.DeleteOAuthExcludedModels)

		mgmt.GET("/oauth-model-alias", h.GetOAuthModelAlias)
		mgmt.PUT("/oauth-model-alias", h.PutOAuthModelAlias)
		mgmt.PATCH("/oauth-model-alias", h.PatchOAuthModelAlias)
		mgmt.DELETE("/oauth-model-alias", h.DeleteOAuthModelAlias)

		mgmt.GET("/auth-files", h.ListAuthFiles)
		mgmt.GET("/auth-files/models", h.GetAuthFileModels)
		mgmt.GET("/model-definitions/:channel", h.GetStaticModelDefinitions)
		mgmt.GET("/image-generation/channels", h.ListImageGenerationChannels)
		mgmt.POST("/image-generation/test", h.PostImageGenerationTest)
		mgmt.GET("/image-generation/test/:task_id", h.GetImageGenerationTestTask)
		mgmt.GET("/auth-files/download", h.DownloadAuthFile)
		mgmt.POST("/auth-files", h.UploadAuthFile)
		mgmt.DELETE("/auth-files", h.DeleteAuthFile)
		mgmt.PATCH("/auth-files/status", h.PatchAuthFileStatus)
		mgmt.PATCH("/auth-files/fields", h.PatchAuthFileFields)
		mgmt.POST("/vertex/import", h.ImportVertexCredential)

		mgmt.GET("/anthropic-auth-url", h.RequestAnthropicToken)
		mgmt.GET("/codex-auth-url", h.RequestCodexToken)
		mgmt.GET("/gemini-cli-auth-url", h.RequestGeminiCLIToken)
		mgmt.GET("/antigravity-auth-url", h.RequestAntigravityToken)
		mgmt.GET("/qwen-auth-url", h.RequestQwenToken)
		mgmt.GET("/kimi-auth-url", h.RequestKimiToken)
		mgmt.GET("/iflow-auth-url", h.RequestIFlowToken)
		mgmt.POST("/iflow-auth-url", h.RequestIFlowCookieToken)
		mgmt.POST("/oauth-callback", h.PostOAuthCallback)
		mgmt.GET("/get-auth-status", h.GetAuthStatus)
	}

	publicMiddlewares := make([]gin.HandlerFunc, 0, 3)
	if opts.Availability != nil {
		publicMiddlewares = append(publicMiddlewares, opts.Availability)
	}
	if opts.PublicNoStore != nil {
		publicMiddlewares = append(publicMiddlewares, opts.PublicNoStore)
	}
	if opts.PublicRateLimit != nil {
		publicMiddlewares = append(publicMiddlewares, opts.PublicRateLimit)
	}

	pub := engine.Group("/v0/management/public")
	pub.Use(publicMiddlewares...)
	{
		pub.GET("/usage", h.GetPublicUsageByAPIKey)
		pub.POST("/usage", h.GetPublicUsageByAPIKey)
		pub.GET("/ccswitch-import-configs", h.GetPublicCcSwitchImportConfigs)
		pub.POST("/ccswitch-import-configs", h.GetPublicCcSwitchImportConfigs)
		pub.GET("/usage/logs", h.GetPublicUsageLogs)
		pub.POST("/usage/logs", h.GetPublicUsageLogs)
		pub.GET("/usage/logs/:id/content", h.GetPublicLogContent)
		pub.POST("/usage/logs/:id/content", h.GetPublicLogContent)
		pub.GET("/usage/chart-data", h.GetPublicUsageChartData)
		pub.POST("/usage/chart-data", h.GetPublicUsageChartData)
		pub.POST("/usage/summary", h.GetPublicUsageSummary)
	}
}
