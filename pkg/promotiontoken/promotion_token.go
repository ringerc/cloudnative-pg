package promotiontoken

import (
	"fmt"
	"strconv"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// TokenVerificationError are raised when the shutdown token
// does not correspond to the status of the current instance
type TokenVerificationError struct {
	msg          string
	retryable    bool
	tokenContent *utils.PgControldataTokenContent
}

// Error implements the error interface
func (e *TokenVerificationError) Error() string {
	return e.msg
}

// IsRetryable is true when this condition is temporary
// and the calling code is expected to retry this
// operator in the future
func (e *TokenVerificationError) IsRetryable() bool {
	return e.retryable
}

func (e *TokenVerificationError) TokenContent() *utils.PgControldataTokenContent {
	return e.tokenContent
}

// ValidateAgainstInstanceStatus checks if the promotion token is valid against the instance status
func ValidateAgainstInstanceStatus(
	promotionToken *utils.PgControldataTokenContent, currentSystemIdentifier string,
	currentTimelineIDString string, replayLSNString string,
) error {
	if err := ValidateAgainstSystemIdentifier(promotionToken, currentSystemIdentifier); err != nil {
		return err
	}

	if err := ValidateAgainstTimelineID(promotionToken, currentTimelineIDString); err != nil {
		return err
	}

	if err := ValidateAgainstLSN(promotionToken, replayLSNString); err != nil {
		return err
	}
	return nil
}

func ValidateAgainstLSN(promotionToken *utils.PgControldataTokenContent, replayLSNString string) error {
	shutdownTokenLSNString := promotionToken.LatestCheckpointREDOLocation
	shutdownTokenLSN, err := postgres.LSN(shutdownTokenLSNString).Parse()
	if err != nil {
		return &TokenVerificationError{
			msg: fmt.Sprintf("promotion token LSN is invalid: %s",
				promotionToken.LatestCheckpointREDOLocation),
			retryable:    false,
			tokenContent: promotionToken,
		}
	}

	replayLSN, err := postgres.LSN(replayLSNString).Parse()
	if err != nil {
		return &TokenVerificationError{
			msg:          fmt.Sprintf("last replay LSN is invalid: %s", replayLSNString),
			retryable:    false,
			tokenContent: promotionToken,
		}
	}

	switch {
	case shutdownTokenLSN < replayLSN:
		return &TokenVerificationError{
			msg: fmt.Sprintf(
				"promotion token LSN (%s) is older than the last replay LSN (%s)",
				shutdownTokenLSNString, replayLSNString),
			retryable:    false,
			tokenContent: promotionToken,
		}

	case replayLSN < shutdownTokenLSN:
		return &TokenVerificationError{
			msg: fmt.Sprintf(
				"waiting for promotion token LSN (%s) to be replayed (the last replayed LSN is %s)",
				shutdownTokenLSNString, replayLSNString),
			retryable:    true,
			tokenContent: promotionToken,
		}
	}

	return nil
}

func ValidateAgainstTimelineID(
	promotionToken *utils.PgControldataTokenContent, currentTimelineIDString string,
) error {
	// If we're in a different timeline, we should definitely wait
	// for this replica to be in the same timeline as the old primary
	shutdownTokenTimeline, err := strconv.Atoi(promotionToken.LatestCheckpointTimelineID)
	if err != nil {
		return &TokenVerificationError{
			msg: fmt.Sprintf("promotion token timeline is not an integer: %s (%s)",
				promotionToken.LatestCheckpointTimelineID, err.Error()),
			retryable:    false,
			tokenContent: promotionToken,
		}
	}

	currentTimelineID, err := strconv.Atoi(currentTimelineIDString)
	if err != nil {
		return &TokenVerificationError{
			msg: fmt.Sprintf("current timeline is not an integer: %s (%s)",
				currentTimelineIDString, err.Error()),
			retryable:    false,
			tokenContent: promotionToken,
		}
	}

	switch {
	case shutdownTokenTimeline > currentTimelineID:
		return &TokenVerificationError{
			msg: fmt.Sprintf("requested timeline not reached, current:%d wanted:%d",
				currentTimelineID, shutdownTokenTimeline),
			retryable:    true,
			tokenContent: promotionToken,
		}

	case shutdownTokenTimeline < currentTimelineID:
		return &TokenVerificationError{
			msg: fmt.Sprintf("requested timeline is older than current one, current:%d wanted:%d",
				currentTimelineID, shutdownTokenTimeline),
			retryable:    false,
			tokenContent: promotionToken,
		}
	}
	return nil
}

func ValidateAgainstSystemIdentifier(
	promotionToken *utils.PgControldataTokenContent, currentSystemIdentifier string,
) error {
	// If the token belongs to a different database, we cannot use if
	if promotionToken.DatabaseSystemIdentifier != currentSystemIdentifier {
		return &TokenVerificationError{
			msg: fmt.Sprintf("mismatching system identifiers, current:%s wanted:%s",
				currentSystemIdentifier, promotionToken.DatabaseSystemIdentifier),
			retryable:    false,
			tokenContent: promotionToken,
		}
	}
	return nil
}
