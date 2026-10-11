package graph

import (
	"log/slog"

	"github.com/kayawari/graphql-todo/services/todo/internal/service"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

type Resolver struct {
	Service *service.TodoService
	Log     *slog.Logger
}
