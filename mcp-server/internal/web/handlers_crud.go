package web

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/thearchitectit/guardrail-mcp/internal/models"
)

func (s *Server) listProjects(c echo.Context) error {
	ctx := c.Request().Context()
	limit, err := strconv.Atoi(c.QueryParam("limit"))
	if err != nil || limit <= 0 || limit > maxPageLimit {
		limit = defaultPageLimit
	}
	offset, err := strconv.Atoi(c.QueryParam("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	projects, err := s.projStore.List(ctx, limit, offset)
	if err != nil {
		slog.Error("Failed to list projects", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to retrieve projects"})
	}

	total, err := s.projStore.Count(ctx)
	if err != nil {
		slog.Warn("Failed to count projects", "error", err)
		total = len(projects) // Fallback to current page size
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": projects,
		"pagination": map[string]interface{}{
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

func (s *Server) getProject(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	proj, err := s.projStore.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, proj)
}

func (s *Server) createProject(c echo.Context) error {
	var proj models.Project
	if err := c.Bind(&proj); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if err := s.projStore.Create(c.Request().Context(), &proj); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, proj)
}

func (s *Server) updateProject(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	var proj models.Project
	if err := c.Bind(&proj); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	proj.ID = parsedUUID
	if err := s.projStore.Update(c.Request().Context(), &proj); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Invalidate cache
	s.cache.InvalidateOnProjectChange(c.Request().Context(), proj.Slug)

	return c.JSON(http.StatusOK, proj)
}

func (s *Server) deleteProject(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	// Get project to find the slug
	proj, err := s.projStore.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	if err := s.projStore.Delete(c.Request().Context(), proj.Slug); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Invalidate cache
	s.cache.InvalidateOnProjectChange(c.Request().Context(), proj.Slug)

	return c.NoContent(http.StatusNoContent)
}

// Failure handlers

func (s *Server) listFailures(c echo.Context) error {
	status := c.QueryParam("status")
	category := c.QueryParam("category")
	projectSlug := c.QueryParam("project")

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > maxPageLimit {
		limit = defaultPageLimit
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}

	failures, err := s.failStore.List(c.Request().Context(), status, category, projectSlug, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"data": failures,
		"pagination": map[string]interface{}{
			"limit":  limit,
			"offset": offset,
			"count":  len(failures),
		},
	})
}

func (s *Server) getFailure(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	failure, err := s.failStore.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, failure)
}

func (s *Server) createFailure(c echo.Context) error {
	var failure models.FailureEntry
	if err := c.Bind(&failure); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	if err := s.failStore.Create(c.Request().Context(), &failure); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, failure)
}

func (s *Server) updateFailure(c echo.Context) error {
	id := c.Param("id")
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id format"})
	}

	var failure models.FailureEntry
	if err := c.Bind(&failure); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	failure.ID = parsedUUID
	if err := s.failStore.Update(c.Request().Context(), &failure); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, failure)
}

// System handlers
