package httpapi

import (
	"errors"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	"recipeworkbench/apps/server/internal/apperr"
	"recipeworkbench/apps/server/internal/service"
)

// handlers 只做参数转交与响应编解码，不含业务判断。
type handlers struct {
	svc *service.Service
}

func (h *handlers) listMachines(c *gin.Context) {
	machines, err := h.svc.Machines(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, machines)
}

func (h *handlers) listRecipes(c *gin.Context) {
	recipes, err := h.svc.Recipes(c.Request.Context(), c.Param("machine"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, recipes)
}

func (h *handlers) getRecipe(c *gin.Context) {
	body, err := h.svc.Recipe(c.Request.Context(), c.Param("machine"), c.Param("recipe"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, body)
}

func (h *handlers) downloadSource(c *gin.Context) {
	name := c.Param("recipe")
	data, err := h.svc.Source(c.Request.Context(), c.Param("machine"), name)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	c.Data(http.StatusOK, "application/octet-stream", data)
}

// writeError 把业务错误类别翻译成 HTTP 语义。
func writeError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if kind, ok := apperr.KindOf(err); ok {
		switch kind {
		case apperr.NotFound:
			status = http.StatusNotFound
		case apperr.Parse:
			status = http.StatusUnprocessableEntity
		case apperr.Invalid:
			status = http.StatusBadRequest
		case apperr.Upstream:
			status = http.StatusBadGateway
		default:
			status = http.StatusInternalServerError
		}
	}
	message := "服务器内部错误"
	var known *apperr.Error
	if errors.As(err, &known) {
		message = known.Message
	}
	c.JSON(status, gin.H{"error": message})
}
