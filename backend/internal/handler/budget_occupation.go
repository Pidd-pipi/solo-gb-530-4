package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"radiation-dose-budget-control/backend/internal/service"
)

type BudgetOccupationHandler struct {
	service *service.BudgetOccupationService
}

func NewBudgetOccupationHandler(service *service.BudgetOccupationService) *BudgetOccupationHandler {
	return &BudgetOccupationHandler{service: service}
}

func (handler *BudgetOccupationHandler) List(context *gin.Context) {
	page, pageSize := Pagination(context)
	items, meta, err := handler.service.List(
		page, pageSize, context.Query("worker_id"), context.Query("plan_id"), context.Query("status"),
	)
	if err != nil {
		WriteError(context, err)
		return
	}
	WritePage(context, items, meta)
}

func (handler *BudgetOccupationHandler) Get(context *gin.Context) {
	id, err := PathID(context)
	if err != nil {
		WriteError(context, err)
		return
	}
	item, err := handler.service.Get(id)
	if err != nil {
		WriteError(context, err)
		return
	}
	WriteData(context, http.StatusOK, item)
}

func (handler *BudgetOccupationHandler) WorkerBalances(context *gin.Context) {
	page, pageSize := Pagination(context)
	items, meta, err := handler.service.WorkerBalances(page, pageSize, context.Query("profile_status"))
	if err != nil {
		WriteError(context, err)
		return
	}
	WritePage(context, items, meta)
}
