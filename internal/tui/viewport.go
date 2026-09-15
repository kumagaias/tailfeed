package tui

import "strings"

// reloadGroupPreservePos switches to m.tabIdx's articles, saving/restoring cursor.
func (m *Model) reloadGroupPreservePos(prevTab int) {
	m.tabCursors[prevTab] = m.cursor
	m.tabOffsets[prevTab] = m.viewport.YOffset
	m.articlesOffset = 0
	m.articlesHasMore = false
	m.articlesLoading = false
	_ = m.reloadArticles()
	if saved, ok := m.tabCursors[m.tabIdx]; ok {
		if saved < len(m.articles) {
			m.cursor = saved
		}
	}
	if offset, ok := m.tabOffsets[m.tabIdx]; ok {
		m.viewport.Width = m.listWidth()
		m.viewport.Height = m.contentHeight()
		m.viewport.SetContent(m.renderArticles())
		m.viewport.SetYOffset(offset)
	} else {
		m.centerViewportOnCursor()
	}
	m.updateDetailContent()
}

func (m *Model) syncViewportToCursor() {
	m.centerViewportOnCursor()
}

func (m *Model) centerViewportOnCursor() {
	m.viewport.Width = m.listWidth()
	m.viewport.Height = m.contentHeight()
	content := m.renderArticles()
	cardTop, cardBottom := m.cursorLineRange()
	cardCenter := cardTop + (cardBottom-cardTop)/2
	offset := cardCenter - m.viewport.Height/2
	if offset < 0 {
		offset = 0
	}
	maxOffset := maxContentOffset(content, m.viewport.Height)
	if offset > maxOffset {
		offset = maxOffset
	}
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(offset)
}

func (m *Model) cursorLineRange() (int, int) {
	return m.articleLineRange(m.cursor)
}

func (m *Model) articleLineRange(idx int) (int, int) {
	if idx < 0 || idx >= len(m.articles) {
		return 0, 0
	}
	innerWidth := m.listWidth() - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	top := 0
	for i, a := range m.articles {
		cardLines := strings.Count(m.renderCard(i, a, innerWidth), "\n") + 1
		if i == idx {
			return top, top + cardLines
		}
		top += cardLines
		if i < len(m.articles)-1 {
			top += articleGapLines
		}
	}
	return 0, 0
}

func (m *Model) articleAtLine(line int) (int, int) {
	if line < 0 {
		return -1, 0
	}
	innerWidth := m.listWidth() - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	top := 0
	for i, a := range m.articles {
		cardLines := strings.Count(m.renderCard(i, a, innerWidth), "\n") + 1
		if line >= top && line < top+cardLines {
			return i, line - top
		}
		top += cardLines
		if i < len(m.articles)-1 {
			if line >= top && line < top+articleGapLines {
				return -1, 0
			}
			top += articleGapLines
		}
	}
	return -1, 0
}

func maxContentOffset(content string, viewportHeight int) int {
	totalLines := strings.Count(content, "\n") + 1
	return max(0, totalLines-viewportHeight)
}

func (m *Model) contentHeight() int {
	if m.mode == modeCommand || m.mode == modeSuggestInput {
		return m.height - 4 - inputBoxHeight
	}
	if m.status != "" {
		return m.height - 5
	}
	return m.height - 4
}

func (m *Model) resizeViewport() {
	h := m.contentHeight()
	m.viewport.Width = m.listWidth()
	m.viewport.Height = h
	m.detailVP.Width = m.detailPaneWidth()
	m.detailVP.Height = h
	m.centerViewportOnCursor()
	m.updateDetailContent()
}

func (m *Model) updateDetailContent() {
	if !m.detailOpen || m.detailPaneWidth() <= 0 {
		return
	}
	m.detailVP.SetYOffset(0)
	m.detailVP.SetContent(m.renderDetailContent())
}
