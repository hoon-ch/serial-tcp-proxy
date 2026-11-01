export function initTabs() {
    const tabs = document.querySelectorAll('.tab-btn');
    const tabContents = document.querySelectorAll('.tab-content');

    tabs.forEach(tab => {
        tab.addEventListener('click', () => {
            tabs.forEach(t => t.classList.remove('active'));
            tabContents.forEach(c => c.classList.remove('active'));

            tab.classList.add('active');
            const targetId = tab.getAttribute('data-tab');
            document.getElementById(`tab-${targetId}`).classList.add('active');
        });
    });
}

export function updateStatus(data) {
    const statusBadge = document.getElementById('connection-status');
    const statusText = statusBadge.querySelector('.text');
    const upstreamAddr = document.getElementById('upstream-addr');
    const listenPort = document.getElementById('listen-port');
    const clientCount = document.getElementById('client-count');
    const uptimeEl = document.getElementById('uptime');

    const isConnected = data.upstream_state === 'Connected';
    statusBadge.classList.toggle('connected', isConnected);
    statusBadge.classList.toggle('disconnected', !isConnected);
    statusText.textContent = data.upstream_state;

    upstreamAddr.textContent = data.upstream_addr;
    adjustFontSize(upstreamAddr);
    listenPort.textContent = data.listen_addr.replace(':', '');
    clientCount.textContent = `${data.connected_clients} / ${data.max_clients}`;

    if (data.start_time) {
        return new Date(data.start_time);
    }
    return null;
}

// Adjust font size based on text length
function adjustFontSize(element) {
    const text = element.textContent;
    const len = text.length;

    if (len > 25) {
        element.style.fontSize = '0.875rem';
    } else if (len > 20) {
        element.style.fontSize = '1rem';
    } else if (len > 15) {
        element.style.fontSize = '1.125rem';
    } else {
        element.style.fontSize = '';  // Reset to default
    }
}

export function updateUptime(startTime) {
    if (!startTime) return;
    const uptimeEl = document.getElementById('uptime');
    const now = new Date();
    const diff = Math.floor((now - startTime) / 1000);

    if (diff < 0) {
        uptimeEl.textContent = "00:00:00";
        return;
    }

    const h = Math.floor(diff / 3600);
    const m = Math.floor((diff % 3600) / 60);
    const s = diff % 60;

    uptimeEl.textContent = `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
}
