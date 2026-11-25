// Package awstarget issues the credentials of grants on AWS. A credential is one session
// of AWS Security Token Service (STS) for the role of the identity of the grant. The
// package starts no program, so the core of the control plane can use it.
package awstarget

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

const (
	// minSession is the shortest session that STS gives.
	minSession = 900 * time.Second
	// maxSession is the longest session that STS gives to a caller that is itself a role,
	// as the control plane is. It is also the maximum of the agent roles.
	maxSession = time.Hour
	// maxSessionName is the limit of STS for the name of a session.
	maxSessionName = 64
	// planTagLength is how much of the plan hash goes into the session tag. It is enough
	// to find the plan in the audit log.
	planTagLength = 12
)

// Session tags. The trust policy of an applier role and of the debugger role of layer
// 00-bootstrap needs the first three. The other two join the trail of the cloud to the
// audit log.
const (
	tagGrant    = "clanker_grant"
	tagEnv      = "clanker_env"
	tagLayer    = "clanker_layer"
	tagWorkItem = "clanker_work_item"
	tagPlan     = "clanker_plan"
)

// STS is the one call of STS that this package uses.
type STS interface {
	AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, options ...func(*sts.Options)) (
		*sts.AssumeRoleOutput, error)
}

// NewSTS returns STS in the region, with the credentials of the role of the instance. It
// makes no call.
func NewSTS(region string) STS {
	return sts.New(sts.Options{Region: region,
		Credentials: aws.NewCredentialsCache(ec2rolecreds.New())})
}

var roleARN = regexp.MustCompile(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/.+$`)

// Credentials issues the credentials of grants on AWS.
type Credentials struct {
	STS STS
	// Roles gives the role ARN of each identity: Bootstrap.Roles.
	Roles map[string]string
	// Record appends an event to the audit log.
	Record func(audit.Event) error
}

var notInSessionName = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// SessionName returns the name of the STS session of a grant: the work item, then the
// grant ID. The cloud trail shows the name for each call of the session. It holds no
// name and no address of a person: the approvers are in the audit log only.
func SessionName(g broker.Grant) string {
	grant := notInSessionName.ReplaceAllString(g.ID, "-")
	if len(grant) > maxSessionName {
		grant = grant[:maxSessionName]
	}
	workItem := notInSessionName.ReplaceAllString(g.WorkItem, "-")
	// The grant ID is what makes the name unique, so only the work item gets shorter.
	if room := maxSessionName - len(grant) - 1; len(workItem) > room {
		workItem = workItem[:max(room, 0)]
	}
	if workItem == "" {
		return grant
	}
	return workItem + "-" + grant
}

// sessionSeconds returns the lifetime to ask STS for.
//
// STS gives no session under 900 seconds. For a grant with less time left, the session
// lives up to that much longer than the grant, and STS cannot end it earlier: see Withdraw.
// A grant with more than one hour left gets a session of one hour, which ends before the
// grant does.
func sessionSeconds(remaining time.Duration) int32 {
	return int32(min(max(remaining, minSession), maxSession) / time.Second)
}

func sessionTags(g broker.Grant) []types.Tag {
	plan := g.PlanSHA256
	if len(plan) > planTagLength {
		plan = plan[:planTagLength]
	}
	pairs := [][2]string{{tagGrant, g.ID}, {tagEnv, g.Env}, {tagLayer, g.Layer},
		{tagWorkItem, g.WorkItem}, {tagPlan, plan}}
	tags := []types.Tag{}
	for _, pair := range pairs {
		// STS refuses a tag with an empty value. A plan grant has no plan hash.
		if pair[1] != "" {
			tags = append(tags, types.Tag{Key: aws.String(pair[0]), Value: aws.String(pair[1])})
		}
	}
	return tags
}

// Issue returns an STS session for the role of the identity of a grant that the broker
// checked. The request has no session policy: the permissions are those of the role, inside
// its permission boundary. Layer 00-bootstrap owns both, and the trust policy of the role.
func (c Credentials) Issue(ctx context.Context, g broker.Grant, remaining time.Duration) (
	broker.Credential, error) {
	role, found := c.Roles[g.Identity]
	if !found {
		return broker.Credential{}, fmt.Errorf("grant %s: layer 00-bootstrap gives no role for "+
			"the identity %s on AWS", g.ID, g.Identity)
	}
	name := SessionName(g)
	out, err := c.STS.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: aws.String(role),
		RoleSessionName: aws.String(name), DurationSeconds: aws.Int32(sessionSeconds(remaining)),
		Tags: sessionTags(g)})
	if err != nil {
		return broker.Credential{}, fmt.Errorf("assume the role %s for grant %s: %w. The role "+
			"of the control plane must be the broker_principal of layer 00-bootstrap and needs "+
			"sts:AssumeRole and sts:TagSession on the role", role, g.ID, err)
	}
	if out.Credentials == nil || out.Credentials.Expiration == nil {
		return broker.Credential{}, fmt.Errorf("assume the role %s for grant %s: STS gave no "+
			"credentials", role, g.ID)
	}
	session := &jobspec.AWSSession{AccessKeyID: aws.ToString(out.Credentials.AccessKeyId),
		SecretAccessKey: aws.ToString(out.Credentials.SecretAccessKey),
		SessionToken:    aws.ToString(out.Credentials.SessionToken),
		Expires:         out.Credentials.Expiration.UTC()}
	expires := session.Expires.Format(time.RFC3339)
	// Without the event nobody can join the session to the grant, so the keys are dropped
	// when the audit log does not take it.
	if err := c.Record(audit.Event{Type: "credential.issued", Actor: "broker", Env: g.Env,
		Layer: g.Layer, WorkItem: g.WorkItem,
		Summary: fmt.Sprintf("STS session %s issued for grant %s", name, g.ID),
		Data: map[string]any{"grant": g.ID, "role_arn": role, "session_name": name,
			"expires": expires}}); err != nil {
		return broker.Credential{}, fmt.Errorf("record the STS session of grant %s: %w", g.ID, err)
	}
	return broker.Credential{AWS: session, Mechanism: fmt.Sprintf(
		"STS AssumeRole for the role %s, session %s, expires %s", role, name, expires)}, nil
}

// Withdraw records that the grant ended. It does not make the session invalid: STS cannot
// revoke one session, and the contract of layer 00-bootstrap gives the broker no permission
// to change a role. A session of the grant works until its own expiry, which can be up to
// 900 seconds after the end of the grant. Withdraw also runs for a grant that got no session.
func (c Credentials) Withdraw(_ context.Context, g broker.Grant) error {
	role, found := c.Roles[g.Identity]
	if !found {
		return nil
	}
	name := SessionName(g)
	if err := c.Record(audit.Event{Type: "credential.left_to_expire", Actor: "broker",
		Env: g.Env, Layer: g.Layer, WorkItem: g.WorkItem,
		Summary: fmt.Sprintf("Grant %s ended. STS cannot revoke the session %s: if it was "+
			"issued, it works until it expires", g.ID, name),
		Data: map[string]any{"grant": g.ID, "role_arn": role, "session_name": name}}); err != nil {
		return fmt.Errorf("record the end of the STS session of grant %s: %w", g.ID, err)
	}
	return nil
}
