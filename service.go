package goususmtp

import (
	"bytes"
	"fmt"
	"time"

	"github.com/go-mail/mail"
	"github.com/indece-official/go-gousu"
	"github.com/namsral/flag"
)

// ServiceName defines the name of smtp service used for dependency injection
const ServiceName = "smtp"

var (
	smtpHost     = flag.String("smtp_host", "127.0.0.1", "")
	smtpPort     = flag.Int("smtp_port", 587, "")
	smtpUser     = flag.String("smtp_user", "", "")
	smtpPassword = flag.String("smtp_password", "", "")
	smtpFrom     = flag.String("smtp_from", "", "")
)

// EmailAttachement defines the base model of an email attachemet
type EmailAttachement struct {
	Filename string
	Mimetype string
	Embedded bool
	Content  []byte
}

// Email defines the base model of an email
type Email struct {
	// From is the name of the sender
	// If empty the config flag 'smtp_from' is used
	From         string
	To           string
	Subject      string
	BodyPlain    string
	BodyHTML     string
	Attachements []EmailAttachement
}

// IService defines the interface of the smtp service
type IService interface {
	gousu.IService

	SendEmail(m *Email) error
}

// Service provides an smtp sender running in a separate thread
type Service struct {
	log        *gousu.Log
	channelIn  chan *mail.Message
	channelOut chan error
	error      error
}

var _ IService = (*Service)(nil)

// Name returns the name of the smtp service from ServiceName
func (s *Service) Name() string {
	return ServiceName
}

// Start starts the SMTP-Sender in a separate thread
func (s *Service) Start() error {
	go func() {
		dialer := mail.NewDialer(*smtpHost, *smtpPort, *smtpUser, *smtpPassword)
		dialer.Timeout = 35 * time.Second
		dialer.RetryFailure = true

		var closer mail.SendCloser
		var err error
		open := false

		s.log.Infof("SMTP-Service started, ready to send emails")

		for {
			select {
			case m, ok := <-s.channelIn:
				if !ok {
					return
				}
				if !open {
					if closer, err = dialer.Dial(); err != nil {
						s.error = err
						// TODO: Retry
						continue
					}
					open = true
					s.error = nil
				}
				if err := mail.Send(closer, m); err != nil {
					closer.Close()
					open = false
					s.channelOut <- err
					continue
				}

				s.channelOut <- nil

			// Close the connection to the SMTP server if no email was sent in
			// the last 30 seconds.
			case <-time.After(30 * time.Second):
				if open {
					if err := closer.Close(); err != nil {
						s.log.Warnf("Can't close smtp connection: %s", err)
					}
					open = false
				}
			}
		}
	}()

	return nil
}

// Stop stops the SMTP-Sender thread
func (s *Service) Stop() error {
	close(s.channelIn)
	close(s.channelOut)

	return nil
}

// Health checks if the MailService is healthy
func (s *Service) Health() error {
	if s.error != nil {
		return s.error
	}

	return nil
}

// SendEmail sents a mail via SMTP
func (s *Service) SendEmail(m *Email) error {
	msg := mail.NewMessage()

	from := m.From
	if from == "" {
		from = *smtpFrom
	}

	msg.SetHeader("From", from)
	msg.SetHeader("To", m.To)
	msg.SetHeader("Subject", m.Subject)

	if m.BodyPlain != "" {
		msg.SetBody("text/plain", m.BodyPlain)
	}

	if m.BodyHTML != "" {
		msg.SetBody("text/html", m.BodyHTML)
	}

	if m.Attachements != nil {
		for i := range m.Attachements {
			attachement := m.Attachements[i]
			reader := bytes.NewReader(attachement.Content)

			if attachement.Embedded {
				msg.EmbedReader(attachement.Filename, reader)
			} else {
				msg.AttachReader(attachement.Filename, reader)
			}
		}
	}

	s.channelIn <- msg

	return <-s.channelOut
}

// NewService if the ServiceFactory for an initialized Service
func NewService(ctx gousu.IContext) gousu.IService {
	return &Service{
		channelIn:  make(chan *mail.Message),
		channelOut: make(chan error),
		log:        gousu.GetLogger(fmt.Sprintf("service.%s", ServiceName)),
	}
}

// Assert NewService fullfills gousu.ServiceFactory
var _ (gousu.ServiceFactory) = NewService
