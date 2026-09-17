package handlers

import "strings"

// chunkText splits text into overlapping chunks for embedding. It packs
// whole paragraphs (split on blank lines) greedily up to chunkSize
// characters, carrying the last `overlap` characters of a chunk into the
// start of the next one so meaning isn't lost across a chunk boundary. A
// single paragraph longer than chunkSize is hard-split (with overlap)
// rather than left oversized.
func chunkText(text string, chunkSize, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = 0
	}

	var chunks []string
	var buf strings.Builder

	flush := func() string {
		if buf.Len() == 0 {
			return ""
		}
		s := strings.TrimSpace(buf.String())
		chunks = append(chunks, s)
		buf.Reset()
		return s
	}

	startWithOverlap := func(prevChunk string) {
		if overlap <= 0 || prevChunk == "" {
			return
		}
		start := len(prevChunk) - overlap
		if start < 0 {
			start = 0
		}
		buf.WriteString(prevChunk[start:])
		buf.WriteString("\n\n")
	}

	hardSplit := func(paragraph string) {
		step := chunkSize - overlap
		if step <= 0 {
			step = chunkSize
		}
		for start := 0; start < len(paragraph); start += step {
			end := start + chunkSize
			if end > len(paragraph) {
				end = len(paragraph)
			}
			chunks = append(chunks, paragraph[start:end])
			if end == len(paragraph) {
				break
			}
		}
	}

	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if len(para) > chunkSize {
			flush()
			hardSplit(para)
			continue
		}

		// Adding this paragraph would overflow the current chunk — flush it
		// and seed the next chunk with the overlap tail of the one just flushed.
		if buf.Len() > 0 && buf.Len()+2+len(para) > chunkSize {
			prev := flush()
			startWithOverlap(prev)
		}

		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(para)
	}
	flush()

	return chunks
}
