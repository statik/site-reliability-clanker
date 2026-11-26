package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// stsForward serves STS AssumeRole. It records the role, the session name, the lifetime and
// the session tags that the core asks for, and gets a session of MinIO with the same
// lifetime. MinIO has no roles: each session has the rights of the user of MinIO.
type stsForward struct {
	record *recorder
	minio  *sts.Client
}

func newSTSForward(opts options, record *recorder) *stsForward {
	return &stsForward{record: record, minio: sts.New(sts.Options{Region: "us-east-1",
		BaseEndpoint: aws.String(opts.minio),
		Credentials: credentials.NewStaticCredentialsProvider(opts.minioKey, opts.minioSecret,
			"")})}
}

type assumeRoleResponse struct {
	XMLName     xml.Name `xml:"https://sts.amazonaws.com/doc/2011-06-15/ AssumeRoleResponse"`
	Credentials struct {
		AccessKeyID     string `xml:"AccessKeyId"`
		SecretAccessKey string `xml:"SecretAccessKey"`
		SessionToken    string `xml:"SessionToken"`
		Expiration      string `xml:"Expiration"`
	} `xml:"AssumeRoleResult>Credentials"`
	AssumedRoleID string `xml:"AssumeRoleResult>AssumedRoleUser>AssumedRoleId"`
	AssumedARN    string `xml:"AssumeRoleResult>AssumedRoleUser>Arn"`
	RequestID     string `xml:"ResponseMetadata>RequestId"`
}

// sessionTags returns the tags of an AssumeRole request: Tags.member.N.Key and .Value.
func sessionTags(form map[string][]string) map[string]string {
	tags := map[string]string{}
	for n := 1; ; n++ {
		prefix := "Tags.member." + strconv.Itoa(n)
		key, found := form[prefix+".Key"]
		if !found || len(key) == 0 {
			return tags
		}
		value := form[prefix+".Value"]
		tags[key[0]] = strings.Join(value, "")
	}
}

func (s *stsForward) serve(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("Action") != "AssumeRole" {
		s.record.add("sts", r.PostForm.Get("Action"), r, nil)
		http.Error(w, "the stand-in serves only AssumeRole of STS", http.StatusBadRequest)
		return
	}
	role, name := r.PostForm.Get("RoleArn"), r.PostForm.Get("RoleSessionName")
	seconds, err := strconv.Atoi(r.PostForm.Get("DurationSeconds"))
	if err != nil {
		http.Error(w, "DurationSeconds is not a number", http.StatusBadRequest)
		return
	}
	s.record.add("sts", "AssumeRole", r, map[string]any{"role_arn": role,
		"session_name": name, "duration_seconds": seconds, "tags": sessionTags(r.PostForm)})
	out, err := s.minio.AssumeRole(r.Context(), &sts.AssumeRoleInput{
		RoleArn: aws.String(role), RoleSessionName: aws.String(name),
		DurationSeconds: aws.Int32(int32(seconds))})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: AssumeRole of MinIO:", err)
		http.Error(w, "MinIO refused AssumeRole: "+err.Error(), http.StatusBadGateway)
		return
	}
	var answer assumeRoleResponse
	answer.Credentials.AccessKeyID = aws.ToString(out.Credentials.AccessKeyId)
	answer.Credentials.SecretAccessKey = aws.ToString(out.Credentials.SecretAccessKey)
	answer.Credentials.SessionToken = aws.ToString(out.Credentials.SessionToken)
	answer.Credentials.Expiration = out.Credentials.Expiration.UTC().Format(time.RFC3339)
	answer.AssumedRoleID, answer.AssumedARN = "AROAFAKEAWS:"+name, role+"/"+name
	answer.RequestID = fmt.Sprintf("fakeaws-%d", time.Now().UnixNano())
	w.Header().Set("Content-Type", "text/xml")
	if err := xml.NewEncoder(w).Encode(answer); err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: answer AssumeRole:", err)
	}
}
