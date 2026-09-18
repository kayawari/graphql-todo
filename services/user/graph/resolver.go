package graph

import (
	"log/slog"

	"github.com/kayawari/jikken/graphql-todo/services/user/internal/service"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

type Resolver struct {
	Users *service.UserService
	Log   *slog.Logger
}
