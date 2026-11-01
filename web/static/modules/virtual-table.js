// Virtual scrolling implementation for packet table
// Renders only visible rows for improved performance

export class VirtualTable {
    constructor(options) {
        this.container = options.container;
        this.tbody = options.tbody;
        this.rowHeight = options.rowHeight || 32;
        this.overscan = options.overscan || 5; // Extra rows above/below viewport
        this.data = [];
        this.renderRow = options.renderRow;
        this.onRowClick = options.onRowClick;

        // State
        this.scrollTop = 0;
        this.containerHeight = 0;
        this.visibleStart = 0;
        this.visibleEnd = 0;

        // DOM elements for virtual scrolling
        this.topSpacer = document.createElement('tr');
        this.topSpacer.className = 'virtual-spacer';
        this.topSpacer.innerHTML = '<td colspan="5"></td>';

        this.bottomSpacer = document.createElement('tr');
        this.bottomSpacer.className = 'virtual-spacer';
        this.bottomSpacer.innerHTML = '<td colspan="5"></td>';

        // Rendered rows pool
        this.rowPool = [];
        this.activeRows = new Map(); // dataIndex -> row element

        // Bind methods
        this.handleScroll = this.handleScroll.bind(this);
        this.handleResize = this.handleResize.bind(this);
        this.handleClick = this.handleClick.bind(this);

        // Setup
        this.init();
    }

    init() {
        // Add spacers to tbody
        this.tbody.appendChild(this.topSpacer);
        this.tbody.appendChild(this.bottomSpacer);

        // Event listeners
        this.container.addEventListener('scroll', this.handleScroll, { passive: true });
        window.addEventListener('resize', this.handleResize);
        this.tbody.addEventListener('click', this.handleClick);

        // Initial measurement
        this.measure();
    }

    measure() {
        this.containerHeight = this.container.clientHeight;
        this.visibleCount = Math.ceil(this.containerHeight / this.rowHeight) + this.overscan * 2;
    }

    handleResize() {
        this.measure();
        this.render();
    }

    handleScroll() {
        this.scrollTop = this.container.scrollTop;
        this.render();
    }

    handleClick(e) {
        const row = e.target.closest('tr');
        if (row && row.dataset.index !== undefined && this.onRowClick) {
            const index = parseInt(row.dataset.index, 10);
            const item = this.data[index];
            if (item) {
                this.onRowClick(row, item, index);
            }
        }
    }

    setData(data) {
        this.data = data;
        this.render();
    }

    getTotalHeight() {
        return this.data.length * this.rowHeight;
    }

    getVisibleRange() {
        const start = Math.max(0, Math.floor(this.scrollTop / this.rowHeight) - this.overscan);
        const end = Math.min(this.data.length, start + this.visibleCount + this.overscan);
        return { start, end };
    }

    render() {
        if (!this.data.length) {
            this.clear();
            return;
        }

        const { start, end } = this.getVisibleRange();
        const totalHeight = this.getTotalHeight();

        // Update spacers
        const topHeight = start * this.rowHeight;
        const bottomHeight = Math.max(0, totalHeight - end * this.rowHeight);

        this.topSpacer.querySelector('td').style.height = `${topHeight}px`;
        this.bottomSpacer.querySelector('td').style.height = `${bottomHeight}px`;

        // Track which rows need to be rendered
        const newActiveRows = new Map();
        const rowsToRemove = new Set(this.activeRows.keys());

        // Render visible rows
        for (let i = start; i < end; i++) {
            const item = this.data[i];
            if (!item) continue;

            rowsToRemove.delete(i);

            let row = this.activeRows.get(i);
            if (!row) {
                // Get row from pool or create new
                row = this.rowPool.pop() || document.createElement('tr');
                row.dataset.index = i;

                // Render row content
                this.renderRow(row, item, i);
            }

            newActiveRows.set(i, row);
        }

        // Remove rows that are no longer visible
        for (const index of rowsToRemove) {
            const row = this.activeRows.get(index);
            if (row && row.parentNode) {
                row.parentNode.removeChild(row);
                // Return to pool (limit pool size)
                if (this.rowPool.length < 100) {
                    this.rowPool.push(row);
                }
            }
        }

        // Insert rows in correct order
        const sortedIndices = Array.from(newActiveRows.keys()).sort((a, b) => a - b);
        let insertBefore = this.bottomSpacer;

        for (let i = sortedIndices.length - 1; i >= 0; i--) {
            const index = sortedIndices[i];
            const row = newActiveRows.get(index);
            if (row.parentNode !== this.tbody || row.nextSibling !== insertBefore) {
                this.tbody.insertBefore(row, insertBefore);
            }
            insertBefore = row;
        }

        this.activeRows = newActiveRows;
        this.visibleStart = start;
        this.visibleEnd = end;
    }

    clear() {
        // Remove all active rows
        for (const row of this.activeRows.values()) {
            if (row.parentNode) {
                row.parentNode.removeChild(row);
            }
            if (this.rowPool.length < 100) {
                this.rowPool.push(row);
            }
        }
        this.activeRows.clear();

        // Reset spacers
        this.topSpacer.querySelector('td').style.height = '0px';
        this.bottomSpacer.querySelector('td').style.height = '0px';
    }

    scrollToIndex(index, align = 'end') {
        if (index < 0 || index >= this.data.length) return;

        const itemTop = index * this.rowHeight;
        const itemBottom = itemTop + this.rowHeight;

        if (align === 'end' || align === 'bottom') {
            // Scroll so item is at bottom
            this.container.scrollTop = itemBottom - this.containerHeight;
        } else if (align === 'start' || align === 'top') {
            // Scroll so item is at top
            this.container.scrollTop = itemTop;
        } else if (align === 'center') {
            // Scroll so item is centered
            this.container.scrollTop = itemTop - (this.containerHeight - this.rowHeight) / 2;
        } else if (align === 'nearest') {
            // Scroll minimum distance to make visible
            if (itemTop < this.scrollTop) {
                this.container.scrollTop = itemTop;
            } else if (itemBottom > this.scrollTop + this.containerHeight) {
                this.container.scrollTop = itemBottom - this.containerHeight;
            }
        }
    }

    scrollToEnd() {
        this.container.scrollTop = this.getTotalHeight();
    }

    isAtBottom() {
        return this.container.scrollHeight - this.container.scrollTop - this.container.clientHeight < 50;
    }

    // Update a specific row (for selection changes, etc.)
    updateRow(index) {
        const row = this.activeRows.get(index);
        if (row && this.data[index]) {
            this.renderRow(row, this.data[index], index);
        }
    }

    // Find row element by data index
    getRowElement(index) {
        return this.activeRows.get(index) || null;
    }

    destroy() {
        this.container.removeEventListener('scroll', this.handleScroll);
        window.removeEventListener('resize', this.handleResize);
        this.tbody.removeEventListener('click', this.handleClick);
        this.clear();
    }
}
