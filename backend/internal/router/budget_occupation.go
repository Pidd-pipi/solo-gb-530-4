package router

import (
	"github.com/gin-gonic/gin"

	"radiation-dose-budget-control/backend/internal/handler"
)

// The occupation ledger is read-only over HTTP: occupations are created,
// retained and released only as part of the assessment submit/review and plan
// archive transactions. Every authenticated role can inspect it; the RPO
// audit trail remains separately role-guarded.
func registerBudgetOccupationRoutes(group *gin.RouterGroup, target *handler.BudgetOccupationHandler) {
	routes := group.Group("/budget-occupations")
	routes.GET("", target.List)
	routes.GET("/worker-balances", target.WorkerBalances)
	routes.GET("/:id", target.Get)
}
