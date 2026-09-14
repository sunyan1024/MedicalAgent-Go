package session

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/service/rageval"
	"net/http"

	"github.com/gin-gonic/gin"
)

type RAGEvaluateResponse struct {
	controller.Response
	Summary rageval.Summary      `json:"summary"`
	Cases   []rageval.CaseResult `json:"cases"`
}

func RAGEvaluate(c *gin.Context) {
	req := new(rageval.EvalRequest)
	res := new(RAGEvaluateResponse)

	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	result, err := rageval.Evaluate(c.Request.Context(), c.GetString("uid"), *req)
	if err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	res.Success()
	res.Summary = result.Summary
	res.Cases = result.Cases
	c.JSON(http.StatusOK, res)
}
