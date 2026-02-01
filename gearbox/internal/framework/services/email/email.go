package email

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"log/slog"
	"net/smtp"
	"strings"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Service handles email sending.
type Service struct {
	db       *database.DB
	logger   *slog.Logger
	baseURL  string
	appName  string
}

// NewService creates a new email service.
func NewService(db *database.DB, logger *slog.Logger, baseURL string) *Service {
	return &Service{
		db:      db,
		logger:  logger,
		baseURL: strings.TrimRight(baseURL, "/"),
		appName: "Gearbox",
	}
}

// IsConfigured checks if SMTP is configured and enabled.
func (s *Service) IsConfigured() bool {
	settings, err := s.db.GetSMTPSettings()
	if err != nil {
		s.logger.Error("failed to get SMTP settings", "error", err)
		return false
	}
	return settings.Enabled && settings.Host != "" && settings.FromAddress != ""
}

// SendEmail sends an email using the configured SMTP settings.
func (s *Service) SendEmail(to, subject, htmlBody, textBody string) error {
	settings, err := s.db.GetSMTPSettings()
	if err != nil {
		return fmt.Errorf("failed to get SMTP settings: %w", err)
	}

	if !settings.Enabled {
		return fmt.Errorf("email is not enabled")
	}

	if settings.Host == "" || settings.FromAddress == "" {
		return fmt.Errorf("SMTP not configured")
	}

	// Build the email
	fromName := settings.FromName
	if fromName == "" {
		fromName = s.appName
	}

	boundary := "==BOUNDARY=="
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("%s <%s>", fromName, settings.FromAddress)
	headers["To"] = to
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = fmt.Sprintf("multipart/alternative; boundary=%q", boundary)

	var msg bytes.Buffer
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")

	// Text part
	if textBody != "" {
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		msg.WriteString(textBody)
		msg.WriteString("\r\n")
	}

	// HTML part
	if htmlBody != "" {
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		msg.WriteString(htmlBody)
		msg.WriteString("\r\n")
	}

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	// Connect and send
	addr := fmt.Sprintf("%s:%d", settings.Host, settings.Port)

	var auth smtp.Auth
	if settings.Username != "" && settings.Password != "" {
		auth = smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)
	}

	if settings.UseTLS {
		// Direct TLS connection (port 465 typically)
		return s.sendWithTLS(addr, auth, settings, to, msg.Bytes())
	}

	if settings.UseStartTLS {
		// STARTTLS connection (port 587 typically)
		return s.sendWithStartTLS(addr, auth, settings, to, msg.Bytes())
	}

	// Plain connection (not recommended)
	return smtp.SendMail(addr, auth, settings.FromAddress, []string{to}, msg.Bytes())
}

func (s *Service) sendWithTLS(addr string, auth smtp.Auth, settings *models.SMTPSettings, to string, msg []byte) error {
	tlsConfig := &tls.Config{
		ServerName: settings.Host,
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect with TLS: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client, err := smtp.NewClient(conn, settings.Host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	if err := client.Mail(settings.FromAddress); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	return client.Quit()
}

func (s *Service) sendWithStartTLS(addr string, auth smtp.Auth, settings *models.SMTPSettings, to string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer func() { _ = client.Close() }()

	tlsConfig := &tls.Config{
		ServerName: settings.Host,
		MinVersion: tls.VersionTLS12,
	}

	if err := client.StartTLS(tlsConfig); err != nil {
		return fmt.Errorf("failed to start TLS: %w", err)
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	if err := client.Mail(settings.FromAddress); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	return client.Quit()
}

// SendTestEmail sends a test email to verify SMTP configuration.
func (s *Service) SendTestEmail(to string) error {
	subject := fmt.Sprintf("%s - Test Email", s.appName)
	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #1e40af; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9fafb; }
        .footer { padding: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>%s</h1>
        </div>
        <div class="content">
            <p>This is a test email to verify your SMTP configuration is working correctly.</p>
            <p>If you received this email, your email settings are configured properly!</p>
        </div>
        <div class="footer">
            <p>This email was sent from %s</p>
        </div>
    </div>
</body>
</html>
`, s.appName, s.appName)

	textBody := fmt.Sprintf(`%s - Test Email

This is a test email to verify your SMTP configuration is working correctly.

If you received this email, your email settings are configured properly!

--
This email was sent from %s
`, s.appName, s.appName)

	err := s.SendEmail(to, subject, htmlBody, textBody)
	if err == nil {
		_ = s.db.UpdateSMTPTestEmailTime()
	}
	return err
}

// SendPasswordResetEmail sends a password reset email.
func (s *Service) SendPasswordResetEmail(user *models.User, token string) error {
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.baseURL, token)
	subject := fmt.Sprintf("%s - Password Reset Request", s.appName)

	data := struct {
		AppName   string
		UserName  string
		ResetURL  string
		ExpiresIn string
	}{
		AppName:   s.appName,
		UserName:  user.DisplayName(),
		ResetURL:  resetURL,
		ExpiresIn: "1 hour",
	}

	htmlTmpl := `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #1e40af; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9fafb; }
        .button { display: inline-block; background: #1e40af; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px; margin: 20px 0; }
        .footer { padding: 20px; text-align: center; color: #666; font-size: 12px; }
        .warning { color: #dc2626; font-size: 14px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>{{.AppName}}</h1>
        </div>
        <div class="content">
            <p>Hello {{.UserName}},</p>
            <p>We received a request to reset your password. Click the button below to create a new password:</p>
            <p style="text-align: center;">
                <a href="{{.ResetURL}}" class="button">Reset Password</a>
            </p>
            <p>Or copy and paste this link into your browser:</p>
            <p style="word-break: break-all; color: #1e40af;">{{.ResetURL}}</p>
            <p class="warning">This link will expire in {{.ExpiresIn}}.</p>
            <p>If you didn't request a password reset, you can safely ignore this email. Your password will remain unchanged.</p>
        </div>
        <div class="footer">
            <p>This email was sent from {{.AppName}}</p>
        </div>
    </div>
</body>
</html>`

	textTmpl := `{{.AppName}} - Password Reset Request

Hello {{.UserName}},

We received a request to reset your password. Visit the link below to create a new password:

{{.ResetURL}}

This link will expire in {{.ExpiresIn}}.

If you didn't request a password reset, you can safely ignore this email. Your password will remain unchanged.

--
This email was sent from {{.AppName}}
`

	htmlBody, err := s.executeTemplate(htmlTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render HTML template: %w", err)
	}

	textBody, err := s.executeTemplate(textTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render text template: %w", err)
	}

	return s.SendEmail(user.Email, subject, htmlBody, textBody)
}

// SendAccountApprovedEmail notifies a user their account has been approved.
func (s *Service) SendAccountApprovedEmail(user *models.User) error {
	loginURL := fmt.Sprintf("%s/login", s.baseURL)
	subject := fmt.Sprintf("%s - Account Approved", s.appName)

	data := struct {
		AppName  string
		UserName string
		Email    string
		LoginURL string
	}{
		AppName:  s.appName,
		UserName: user.DisplayName(),
		Email:    user.Email,
		LoginURL: loginURL,
	}

	htmlTmpl := `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #059669; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9fafb; }
        .button { display: inline-block; background: #059669; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px; margin: 20px 0; }
        .footer { padding: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Account Approved!</h1>
        </div>
        <div class="content">
            <p>Hello {{.UserName}},</p>
            <p>Great news! Your account request for {{.AppName}} has been approved.</p>
            <p>You can now log in using your email address: <strong>{{.Email}}</strong></p>
            <p style="text-align: center;">
                <a href="{{.LoginURL}}" class="button">Log In Now</a>
            </p>
        </div>
        <div class="footer">
            <p>This email was sent from {{.AppName}}</p>
        </div>
    </div>
</body>
</html>`

	textTmpl := `Account Approved!

Hello {{.UserName}},

Great news! Your account request for {{.AppName}} has been approved.

You can now log in using your email address: {{.Email}}

Log in at: {{.LoginURL}}

--
This email was sent from {{.AppName}}
`

	htmlBody, err := s.executeTemplate(htmlTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render HTML template: %w", err)
	}

	textBody, err := s.executeTemplate(textTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render text template: %w", err)
	}

	return s.SendEmail(user.Email, subject, htmlBody, textBody)
}

// SendAccountDeniedEmail notifies a user their account request was denied.
func (s *Service) SendAccountDeniedEmail(email, name, reason string) error {
	subject := fmt.Sprintf("%s - Account Request Update", s.appName)

	data := struct {
		AppName  string
		UserName string
		Reason   string
	}{
		AppName:  s.appName,
		UserName: name,
		Reason:   reason,
	}

	htmlTmpl := `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #dc2626; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9fafb; }
        .footer { padding: 20px; text-align: center; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Account Request Update</h1>
        </div>
        <div class="content">
            <p>Hello {{.UserName}},</p>
            <p>We regret to inform you that your account request for {{.AppName}} was not approved at this time.</p>
            {{if .Reason}}
            <p><strong>Reason:</strong> {{.Reason}}</p>
            {{end}}
            <p>If you believe this was an error, please contact the system administrator.</p>
        </div>
        <div class="footer">
            <p>This email was sent from {{.AppName}}</p>
        </div>
    </div>
</body>
</html>`

	textTmpl := `Account Request Update

Hello {{.UserName}},

We regret to inform you that your account request for {{.AppName}} was not approved at this time.
{{if .Reason}}
Reason: {{.Reason}}
{{end}}
If you believe this was an error, please contact the system administrator.

--
This email was sent from {{.AppName}}
`

	htmlBody, err := s.executeTemplate(htmlTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render HTML template: %w", err)
	}

	textBody, err := s.executeTemplate(textTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render text template: %w", err)
	}

	return s.SendEmail(email, subject, htmlBody, textBody)
}

// SendNewAccountRequestEmail notifies admins of a new account request.
func (s *Service) SendNewAccountRequestEmail(adminEmail string, request *models.AccountRequest) error {
	reviewURL := fmt.Sprintf("%s/settings/users", s.baseURL)
	subject := fmt.Sprintf("%s - New Account Request", s.appName)

	data := struct {
		AppName      string
		RequestEmail string
		RequestName  string
		Reason       string
		ReviewURL    string
	}{
		AppName:      s.appName,
		RequestEmail: request.Email,
		RequestName:  request.FirstName + " " + request.LastName,
		Reason:       request.Reason,
		ReviewURL:    reviewURL,
	}

	htmlTmpl := `
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background: #f59e0b; color: white; padding: 20px; text-align: center; }
        .content { padding: 20px; background: #f9fafb; }
        .button { display: inline-block; background: #f59e0b; color: white; padding: 12px 24px; text-decoration: none; border-radius: 4px; margin: 20px 0; }
        .footer { padding: 20px; text-align: center; color: #666; font-size: 12px; }
        .details { background: white; padding: 15px; border-radius: 4px; margin: 15px 0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>New Account Request</h1>
        </div>
        <div class="content">
            <p>A new account request has been submitted for {{.AppName}}.</p>
            <div class="details">
                <p><strong>Name:</strong> {{.RequestName}}</p>
                <p><strong>Email:</strong> {{.RequestEmail}}</p>
                {{if .Reason}}<p><strong>Reason:</strong> {{.Reason}}</p>{{end}}
            </div>
            <p style="text-align: center;">
                <a href="{{.ReviewURL}}" class="button">Review Request</a>
            </p>
        </div>
        <div class="footer">
            <p>This email was sent from {{.AppName}}</p>
        </div>
    </div>
</body>
</html>`

	textTmpl := `New Account Request for {{.AppName}}

A new account request has been submitted:

Name: {{.RequestName}}
Email: {{.RequestEmail}}
{{if .Reason}}Reason: {{.Reason}}{{end}}

Review this request at: {{.ReviewURL}}

--
This email was sent from {{.AppName}}
`

	htmlBody, err := s.executeTemplate(htmlTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render HTML template: %w", err)
	}

	textBody, err := s.executeTemplate(textTmpl, data)
	if err != nil {
		return fmt.Errorf("failed to render text template: %w", err)
	}

	return s.SendEmail(adminEmail, subject, htmlBody, textBody)
}

func (s *Service) executeTemplate(tmplStr string, data interface{}) (string, error) {
	tmpl, err := template.New("email").Parse(tmplStr)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
