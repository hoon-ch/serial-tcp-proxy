export function initInjection() {
    const toggleInjectBtn = document.getElementById('toggle-inject');
    const injectPanel = document.getElementById('inject-panel');
    const sendPacketBtn = document.getElementById('send-packet');
    const injectTarget = document.getElementById('inject-target');
    const injectFormat = document.getElementById('inject-format');
    const injectData = document.getElementById('inject-data');

    toggleInjectBtn.addEventListener('click', () => {
        if (injectPanel.style.display === 'none') {
            injectPanel.style.display = 'block';
            toggleInjectBtn.classList.add('active');
            injectData.focus();
        } else {
            injectPanel.style.display = 'none';
            toggleInjectBtn.classList.remove('active');
        }
    });

    sendPacketBtn.addEventListener('click', async () => {
        const target = injectTarget.value;
        const format = injectFormat.value;
        const data = injectData.value.trim();

        if (!data) {
            alert('Please enter data to send');
            return;
        }

        try {
            const response = await fetch('/api/inject', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    target,
                    format,
                    data
                })
            });

            if (!response.ok) {
                const errText = await response.text();
                throw new Error(errText);
            }

            const originalText = sendPacketBtn.innerText;
            sendPacketBtn.innerText = 'Sent!';
            sendPacketBtn.classList.add('btn-success');
            setTimeout(() => {
                sendPacketBtn.innerText = originalText;
                sendPacketBtn.classList.remove('btn-success');
            }, 1000);

        } catch (err) {
            alert('Failed to send packet: ' + err.message);
        }
    });
}
