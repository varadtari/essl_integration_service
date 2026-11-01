package models

type User struct {
	Id           int
	GymId        *int
	Name         string
	Email        string
	PasswordHash string
	Role         string
	Active       bool
}

type CreateUserRequestBody struct {
	Name     string `json:"name" binding:"required,min=1,max=120"`
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type UserResponse struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

type UserPathParam struct {
	UserId int `uri:"userId" binding:"required,min=1"`
}

type SetUserActiveRequestBody struct {
	Active *bool `json:"active" binding:"required"`
}
