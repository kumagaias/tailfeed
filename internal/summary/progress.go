package summary

// ProgressEvent describes one summary chunk before or after a network call.
type ProgressEvent struct {
	Phase         string
	Attempt       int
	MaxAttempts   int
	ChunkStart    int
	ChunkEnd      int
	TotalArticles int
}

// ProgressFunc receives progress updates while chunked summaries run.
type ProgressFunc func(ProgressEvent)

func notifySummaryProgress(progress ProgressFunc, phase string, attempt, maxAttempts, start, count, total int) {
	if progress == nil {
		return
	}
	end := start + count - 1
	if end > total {
		end = total
	}
	progress(ProgressEvent{
		Phase:         phase,
		Attempt:       attempt,
		MaxAttempts:   maxAttempts,
		ChunkStart:    start,
		ChunkEnd:      end,
		TotalArticles: total,
	})
}
