package auth

const (
	// tokenType is the Authorization scheme: "Bearer <token>".
	tokenType = "Bearer"

	// dummyHash keeps an unknown-email login as slow as a wrong password.
	dummyHash = "$2a$10$CHItcZdGTaN.9ZYG1ip71uSOWV2Y37Q.UnoDB8ztGxT5fC17mKYCy"

	// Login failure reasons, for the log only.
	reasonUnknownEmail  = "unknown_email"
	reasonWrongPassword = "wrong_password"
)
