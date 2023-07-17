package s3

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"golang.org/x/exp/slices"

	"aws-in-a-box/awserrors"
)

type Object struct {
	Data        []byte
	MD5         [16]byte
	ContentType string

	Tagging string

	ServerSideEncryption    string
	SSECustomerAlgorithm    string
	SSECustomerKey          string
	SSEKMSKeyId             string
	SSEKMSEncryptionContext string
}

type Bucket struct {
	objects map[string]*Object
}

type multipartUpload struct {
	Bucket string
	Key    string
	Parts  map[int]Part
	// For metadata
	Object Object
}

type Part struct {
	Data []byte
	MD5  [16]byte
}

type S3 struct {
	// We need the address to generate location URLs.
	addr string

	mu               sync.Mutex
	buckets          map[string]*Bucket
	multipartUploads map[string]*multipartUpload
}

func New(addr string) *S3 {
	return &S3{
		addr:             addr,
		buckets:          make(map[string]*Bucket),
		multipartUploads: make(map[string]*multipartUpload),
	}
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_CreateBucket.html
func (s *S3) CreateBucket(input CreateBucketInput) (*CreateBucketOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.buckets[input.Bucket]
	if ok {
		return nil, awserrors.XXX_TODO("bucket already exists")
	}

	s.buckets[input.Bucket] = &Bucket{
		objects: make(map[string]*Object),
	}

	return &CreateBucketOutput{
		Location: "/" + input.Bucket,
	}, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetObject.html
func (s *S3) GetObject(bucket string, key string) (*Object, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	object, ok := b.objects[key]
	if !ok {
		return nil, awserrors.XXX_TODO("no item")
	}

	fmt.Println("OBJECT", object)
	return object, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html
func (s *S3) PutObject(input PutObjectInput) (*PutObjectOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	object := &Object{
		Data:        input.Data,
		MD5:         md5.Sum(input.Data),
		ContentType: input.ContentType,

		Tagging:              input.Tagging,
		ServerSideEncryption: input.ServerSideEncryption,
		SSEKMSKeyId:          input.SSEKMSKeyId,
		SSECustomerAlgorithm: input.SSECustomerAlgorithm,
		SSECustomerKey:       input.SSECustomerKey,
	}
	b.objects[input.Key] = object

	return &PutObjectOutput{
		Etag:                    hex.EncodeToString(object.MD5[:]),
		SSECustomerAlgorithm:    input.SSECustomerAlgorithm,
		SSEKMSKeyId:             input.SSEKMSKeyId,
		SSEKMSEncryptionContext: input.SSEKMSEncryptionContext,
	}, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_CopyObject.html
func (s *S3) CopyObject(input CopyObjectInput) (*CopyObjectOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// "/bucket/path/to/key"
	copySource, err := url.PathUnescape(input.CopySource)
	if err != nil {
		return nil, awserrors.XXX_TODO(err.Error())
	}
	parts := strings.SplitN(copySource, "/", 3)
	sourceBucket := parts[1]
	sourceKey := parts[2]

	b, ok := s.buckets[sourceBucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	object, ok := b.objects[sourceKey]
	if !ok {
		return nil, awserrors.XXX_TODO("no source item")
	}

	if input.MetadataDirective == "REPLACE" {
		// See https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingMetadata.html for full list
		object.ContentType = input.ContentType
		object.ServerSideEncryption = input.ServerSideEncryption
		object.SSEKMSKeyId = input.SSEKMSKeyId
		object.SSECustomerAlgorithm = input.SSECustomerAlgorithm
		object.SSECustomerKey = input.SSECustomerKey
	}

	if input.TaggingDirective == "REPLACE" {
		object.Tagging = input.Tagging
	}

	destBucket, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	destBucket.objects[input.Key] = object
	return &CopyObjectOutput{
		// TODO: Complete guess on format
		LastModified: time.Now().UTC().Format(time.RFC3339Nano),
		ETag:         hex.EncodeToString(object.MD5[:]),
	}, nil
}

func etag(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_DeleteObject.html
func (s *S3) DeleteObject(input DeleteObjectInput) (*DeleteObjectOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	_, ok = b.objects[input.Key]
	if !ok {
		return nil, awserrors.XXX_TODO("no item")
	}

	delete(b.objects, input.Key)
	return nil, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetObjectTagging.html
func (s *S3) GetObjectTagging(input GetObjectTaggingInput) (*GetObjectTaggingOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	object, ok := b.objects[input.Key]
	if !ok {
		return nil, awserrors.XXX_TODO("no item")
	}

	tagging := &GetObjectTaggingOutput{}
	for _, kv := range strings.Split(object.Tagging, "&") {
		kv := strings.Split(kv, "=")
		if len(kv) != 2 {
			return nil, awserrors.XXX_TODO("invalid tagging")
		}
		tagging.TagSet.Tag = append(tagging.TagSet.Tag, APITag{
			Key:   kv[0],
			Value: kv[1],
		})
	}
	return tagging, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObjectTagging.html
func (s *S3) PutObjectTagging(input PutObjectTaggingInput) (*PutObjectTaggingOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	object, ok := b.objects[input.Key]
	if !ok {
		return nil, awserrors.XXX_TODO("no item")
	}

	tagging := strings.Builder{}
	for i, tag := range input.TagSet.Tag {
		tagging.WriteString(tag.Key)
		tagging.WriteRune('=')
		tagging.WriteString(tag.Value)
		if i != len(input.TagSet.Tag)-1 {
			tagging.WriteRune(',')
		}
	}
	object.Tagging = tagging.String()

	return &PutObjectTaggingOutput{}, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_DeleteObjectTagging.html
func (s *S3) DeleteObjectTagging(input DeleteObjectTaggingInput) (*Response204, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	object, ok := b.objects[input.Key]
	if !ok {
		return nil, awserrors.XXX_TODO("no item")
	}
	object.Tagging = ""

	return response204, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_CreateMultipartUpload.html
func (s *S3) CreateMultipartUpload(input CreateMultipartUploadInput) (*CreateMultipartUploadOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.buckets[input.Bucket]
	if !ok {
		return nil, awserrors.XXX_TODO("no bucket")
	}

	uploadId := base64.RawURLEncoding.EncodeToString(uuid.Must(uuid.NewV4()).Bytes())

	s.multipartUploads[uploadId] = &multipartUpload{
		Bucket: input.Bucket,
		Key:    input.Key,
		Parts:  make(map[int]Part),
		// Just for metadata
		Object: Object{
			ContentType:             input.ContentType,
			ServerSideEncryption:    input.ServerSideEncryption,
			SSEKMSKeyId:             input.SSEKMSKeyId,
			SSEKMSEncryptionContext: input.SSEKMSEncryptionContext,
		},
	}

	return &CreateMultipartUploadOutput{
		Bucket:   input.Bucket,
		Key:      input.Key,
		UploadId: uploadId,
	}, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_UploadPart.html
func (s *S3) UploadPart(input UploadPartInput) (*UploadPartOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	upload, ok := s.multipartUploads[input.UploadId]
	if !ok {
		return nil, awserrors.XXX_TODO("no upload")
	}

	if upload.Bucket != input.Bucket || upload.Key != input.Key {
		return nil, awserrors.XXX_TODO("wrong upload")
	}

	part := Part{
		Data: input.Data,
		MD5:  md5.Sum(input.Data),
	}
	upload.Parts[input.PartNumber] = part
	return &UploadPartOutput{
		ETag:                 hex.EncodeToString(part.MD5[:]),
		ServerSideEncryption: upload.Object.ServerSideEncryption,
		SSEKMSKeyId:          upload.Object.SSEKMSKeyId,
	}, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_CompleteMultipartUpload.html
func (s *S3) CompleteMultipartUpload(input CompleteMultipartUploadInput) (*CompleteMultipartUploadOutput, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	upload, ok := s.multipartUploads[input.UploadId]
	if !ok {
		return nil, awserrors.XXX_TODO("no upload")
	}

	if upload.Bucket != input.Bucket || upload.Key != input.Key {
		return nil, awserrors.XXX_TODO("wrong upload")
	}

	slices.SortFunc(input.Part, func(a, b APIPart) bool {
		return a.PartNumber < b.PartNumber
	})

	var combinedMD5s []byte
	combinedDataLen := 0
	for _, partSpec := range input.Part {
		part, ok := upload.Parts[partSpec.PartNumber]
		if !ok {
			return nil, awserrors.XXX_TODO("missing part")
		}

		if partSpec.ETag != hex.EncodeToString(part.MD5[:]) {
			return nil, awserrors.XXX_TODO("wrong part")
		}

		combinedMD5s = append(combinedMD5s, part.MD5[:]...)
		combinedDataLen += len(part.Data)
	}

	combinedData := make([]byte, 0, combinedDataLen)
	for _, partSpec := range input.Part {
		part := upload.Parts[partSpec.PartNumber]
		combinedData = append(combinedData, part.Data...)
	}

	object := upload.Object
	object.Data = combinedData
	s.buckets[input.Bucket].objects[input.Key] = &object
	delete(s.multipartUploads, input.UploadId)

	return &CompleteMultipartUploadOutput{
		Bucket:               input.Bucket,
		Key:                  input.Key,
		Location:             fmt.Sprintf("http://%s/%s/%s", s.addr, input.Bucket, input.Key),
		ETag:                 etag(combinedMD5s) + "-" + strconv.Itoa(len(input.Part)),
		ServerSideEncryption: object.ServerSideEncryption,
		SSEKMSKeyId:          object.SSEKMSKeyId,
	}, nil
}

// https://docs.aws.amazon.com/AmazonS3/latest/API/API_AbortMultipartUpload.html
func (s *S3) AbortMultipartUpload(input AbortMultipartUploadInput) (*Response204, *awserrors.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.multipartUploads, input.UploadId)
	return response204, nil
}