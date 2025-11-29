// Client management module

const clientsCard = document.getElementById('clients-card');
const clientsModal = document.getElementById('clients-modal');
const closeClientsModalBtn = document.getElementById('close-clients-modal');
const clientsList = document.getElementById('clients-list');
const noClientsMessage = document.getElementById('no-clients-message');

// Summary elements
const modalTcpCount = document.getElementById('modal-tcp-count');
const modalWebCount = document.getElementById('modal-web-count');
const modalTotalCount = document.getElementById('modal-total-count');
const modalMaxClients = document.getElementById('modal-max-clients');

let isModalOpen = false;
let refreshInterval = null;

// Format connected time
function formatConnectedTime(isoString) {
    const date = new Date(isoString);
    const now = new Date();
    const diff = Math.floor((now - date) / 1000);

    if (diff < 60) return `${diff}s ago`;
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return date.toLocaleString();
}

// Fetch and render clients
async function fetchClients() {
    try {
        const response = await fetch('/api/clients');
        if (!response.ok) throw new Error('Failed to fetch clients');

        const data = await response.json();
        renderClients(data);
    } catch (error) {
        console.error('Error fetching clients:', error);
    }
}

// Render clients in the table
function renderClients(data) {
    // Update summary
    modalTcpCount.textContent = data.tcp_count;
    modalWebCount.textContent = data.web_count;
    modalTotalCount.textContent = data.total_count;
    modalMaxClients.textContent = data.max_clients;

    // Clear existing rows
    clientsList.innerHTML = '';

    if (data.clients.length === 0) {
        noClientsMessage.style.display = 'block';
        return;
    }

    noClientsMessage.style.display = 'none';

    // Add client rows
    data.clients.forEach(client => {
        const row = document.createElement('tr');
        row.innerHTML = `
            <td class="client-id">${client.id}</td>
            <td class="client-addr">${client.addr}</td>
            <td><span class="client-type ${client.type}">${client.type}</span></td>
            <td class="client-time">${formatConnectedTime(client.connected_at)}</td>
            <td>
                <button class="btn-disconnect" data-client-id="${client.id}">
                    Disconnect
                </button>
            </td>
        `;
        clientsList.appendChild(row);
    });

    // Add disconnect handlers
    clientsList.querySelectorAll('.btn-disconnect').forEach(btn => {
        btn.addEventListener('click', () => disconnectClient(btn.dataset.clientId));
    });
}

// Disconnect a client
async function disconnectClient(clientId) {
    if (!confirm(`Disconnect ${clientId}?`)) return;

    try {
        const response = await fetch('/api/clients/disconnect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ client_id: clientId })
        });

        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }

        // Refresh the list
        fetchClients();
    } catch (error) {
        console.error('Error disconnecting client:', error);
        alert(`Failed to disconnect: ${error.message}`);
    }
}

// Open modal
function openModal() {
    isModalOpen = true;
    clientsModal.style.display = 'flex';
    fetchClients();

    // Start auto-refresh every 2 seconds
    refreshInterval = setInterval(fetchClients, 2000);
}

// Close modal
function closeModal() {
    isModalOpen = false;
    clientsModal.style.display = 'none';

    // Stop auto-refresh
    if (refreshInterval) {
        clearInterval(refreshInterval);
        refreshInterval = null;
    }
}

// Initialize
export function initClients() {
    // Card click handler
    clientsCard.addEventListener('click', openModal);

    // Close modal button
    closeClientsModalBtn.addEventListener('click', closeModal);

    // Close on outside click
    clientsModal.addEventListener('click', (e) => {
        if (e.target === clientsModal) {
            closeModal();
        }
    });

    // Close on Escape key
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && isModalOpen) {
            closeModal();
        }
    });
}
