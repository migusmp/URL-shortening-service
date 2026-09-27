package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/service"
)

type URLHandler struct {
	svc *service.URLService
}

func NewURLHandler(svc *service.URLService) *URLHandler {
	return &URLHandler{svc: svc}
}

type createRequest struct {
	LongURL    string `json:"long_url" binding:"required"`
	CustomCode string `json:"custom_code"`
}

type updateRequest struct {
	LongURL    *string `json:"long_url"`
	CustomCode *string `json:"custom_code"`
}

func (h *URLHandler) Create(c *gin.Context) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	u, err := h.svc.Create(c.Request.Context(), service.CreateInput{
		LongURL:    req.LongURL,
		CustomCode: req.CustomCode,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, u)
}

func (h *URLHandler) Get(c *gin.Context) {
	code := c.Param("code")
	u, err := h.svc.Get(c.Request.Context(), code)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func (h *URLHandler) Update(c *gin.Context) {
	code := c.Param("code")
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.LongURL == nil && req.CustomCode == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one of long_url or custom_code is required"})
		return
	}

	u, err := h.svc.Update(c.Request.Context(), code, service.UpdateInput{
		LongURL:    req.LongURL,
		CustomCode: req.CustomCode,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func (h *URLHandler) Delete(c *gin.Context) {
	code := c.Param("code")
	if err := h.svc.Delete(c.Request.Context(), code); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *URLHandler) Redirect(c *gin.Context) {
	code := c.Param("code")
	longURL, err := h.svc.ResolveRedirect(c.Request.Context(), code)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Redirect(http.StatusFound, longURL)
}

func (h *URLHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, domain.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "short code already exists"})
	case errors.Is(err, domain.ErrInvalidURL), errors.Is(err, domain.ErrInvalidCode):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrGenerateCode):
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not generate short code"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
