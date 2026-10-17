package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func main() {
	brokers := flag.String("brokers", "localhost:9092", "comma-separated list of Kafka brokers")
	group := flag.String("group", "go-cross-consumer", "consumer group id")
	topic := flag.String("topic", "demo", "topic to consume")
	flag.Parse()

	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": *brokers,
		"group.id":          *group,
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		log.Fatalf("create consumer: %v", err)
	}
	defer c.Close()

	if err := c.SubscribeTopics([]string{*topic}, nil); err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	log.Printf("consuming %q from %s as group %q (Ctrl+C to stop)", *topic, *brokers, *group)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case sig := <-sigs:
			log.Printf("caught %v, shutting down", sig)
			return
		default:
		}

		msg, err := c.ReadMessage(100 * time.Millisecond)
		if err != nil {
			var kerr kafka.Error
			if errors.As(err, &kerr) && kerr.Code() == kafka.ErrTimedOut {
				continue
			}
			log.Printf("consumer error: %v", err)
			continue
		}

		log.Printf("%s [%d] @%d key=%q value=%q",
			*msg.TopicPartition.Topic, msg.TopicPartition.Partition, msg.TopicPartition.Offset,
			msg.Key, msg.Value)
	}
}
