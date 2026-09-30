package application

import "errors"

var (
	ErrTicketNotFound     = errors.New("ticket not found")
	ErrTicketTitleEmpty   = errors.New("ticket title is required")
	ErrTicketContentEmpty = errors.New("ticket content is required")
	ErrTicketClosed       = errors.New("ticket is closed")
	ErrTicketNotClosed    = errors.New("ticket is not closed")
	ErrTicketPriorityBad  = errors.New("ticket priority invalid")
	ErrTicketOrderInvalid = errors.New("ticket order invalid or not owned by user")
)
