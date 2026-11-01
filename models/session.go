package models

// Session is the caller identity AuthorizeSession puts in the gin context. GymId is nil only for
// superadmins; every gym-scoped query takes its gym id from here, never from the request.
type Session struct {
	UserId int
	GymId  *int
	Role   string
}

type LoginRequestBody struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=1,max=128"`
}

type MeResponse struct {
	UserId  int     `json:"userId"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Role    string  `json:"role"`
	GymId   *int    `json:"gymId"`
	GymName *string `json:"gymName"`
}
