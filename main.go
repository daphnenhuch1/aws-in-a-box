package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"aws-in-a-box/arn"
	"aws-in-a-box/services/kinesis"
	"aws-in-a-box/services/kms"
)

func main() {
	addr := flag.String("addr", "localhost:4569", "Address to run on")

	enableKinesis := flag.Bool("enableKinesis", true, "Enable Kinesis service")
	kinesisInitialStreams := flag.String("kinesisInitialStreams", "",
		"Streams to create at startup. Example: stream1,stream2,stream3")
	kinesisInitialShardsPerStream := flag.Int64("kinesisInitialShardsPerStream", 2,
		"How many shards to create for each stream listed in -kinesisInitialStreams")
	kinesisDefaultDuration := flag.Duration("kinesisDefaultDuration", 24*time.Hour,
		"How long to retain messages. Can be used to control memory usage. After creation, retention can be adjusted with [Increase/Decrease]StreamRetentionPeriod")

	enableKMS := flag.Bool("enableKMS", true, "Enable Kinesis service")

	flag.Parse()

	methodRegistry := make(map[string]http.HandlerFunc)

	arnGenerator := arn.Generator{
		// TODO: make these configurable?
		AwsAccountId: "12345",
		Region:       "us-east-1",
	}

	if *enableKinesis {
		k := kinesis.New(arnGenerator, *kinesisDefaultDuration)
		for _, name := range strings.Split(*kinesisInitialStreams, ",") {
			k.CreateStream(kinesis.CreateStreamInput{
				StreamName: name,
				ShardCount: *kinesisInitialShardsPerStream,
			})
		}
		k.RegisterHTTPHandlers(methodRegistry)
		log.Println("Enabled Kinesis")
	}

	if *enableKMS {
		k := kms.New(arnGenerator)
		k.RegisterHTTPHandlers(methodRegistry)
		log.Println("Enabled KMS")
	}


	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			log.Print("bodyErr ", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(buf))

		// The target endpoint is specified in the `X-Amz-Target` header.
		target := r.Header.Get("X-Amz-Target")
		log.Println(r.Method, r.URL.String(), target) //, r.Body)

		w.Header().Add("x-amzn-RequestId", uuid.Must(uuid.NewV4()).String())
		method, ok := methodRegistry[target]
		if !ok {
			fmt.Println("NOT FOUND")
			w.WriteHeader(404)
			return
		}
		method(w, r)
	})

	h2s := &http2.Server{}
	h1s := &http.Server{
		Addr:    *addr,
		Handler: h2c.NewHandler(handler, h2s),
	}

	err := h1s.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
