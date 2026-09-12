package mock

import (
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/mirror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// topicStream is the mirror SubscribeTopic endpoint. It replays stored
// messages from the requested start time and then follows new ones until the
// client goes away, the limit is reached or the end time has passed.
type topicStream struct {
	mirror.UnimplementedConsensusServiceServer
	ledger *ledger
}

func (s *topicStream) SubscribeTopic(q *mirror.ConsensusTopicQuery, stream grpc.ServerStreamingServer[mirror.ConsensusTopicResponse]) error {
	l := s.ledger
	id := q.GetTopicID()
	if id == nil {
		return status.Error(codes.InvalidArgument, "topic id is required")
	}

	l.mu.Lock()
	if _, c := l.st.topic(id); c != codeOK {
		l.mu.Unlock()
		return status.Error(codes.NotFound, "topic does not exist")
	}
	l.mu.Unlock()

	start := timeFromProto(q.GetConsensusStartTime())
	var sent uint64
	var next int // index of the next message to look at
	for {
		l.mu.Lock()
		tp := l.st.topics[id.TopicNum]
		batch := tp.messages[next:]
		next = len(tp.messages)
		wake := l.changed
		l.mu.Unlock()

		for _, m := range batch {
			if m.consensus.Before(start) {
				continue
			}
			if end := q.GetConsensusEndTime(); end != nil && !m.consensus.Before(timeFromProto(end)) {
				return nil
			}
			if err := stream.Send(topicResponse(m)); err != nil {
				return err
			}
			sent++
			if q.Limit > 0 && sent >= q.Limit {
				return nil
			}
		}

		select {
		case <-wake:
		case <-stream.Context().Done():
			return nil
		case <-l.done:
			return nil
		}
	}
}

func topicResponse(m *topicMessage) *mirror.ConsensusTopicResponse {
	return &mirror.ConsensusTopicResponse{
		ConsensusTimestamp: timestampProto(m.consensus),
		Message:            m.message,
		RunningHash:        m.runningHash,
		SequenceNumber:     m.sequence,
		RunningHashVersion: runningHashVersion,
		ChunkInfo:          m.chunk,
	}
}
