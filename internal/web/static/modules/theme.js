export function initTheme() {
    const themeToggleBtn = document.getElementById('theme-toggle');
    const icon = themeToggleBtn.querySelector('span'); // Assuming we use text or icon

    // Check saved preference
    const savedTheme = localStorage.getItem('theme');
    const systemDark = window.matchMedia('(prefers-color-scheme: dark)');

    function applyTheme(theme) {
        if (theme === 'dark') {
            document.documentElement.setAttribute('data-theme', 'dark');
            icon.textContent = '☀️'; // Button shows option to switch to Light
        } else if (theme === 'light') {
            document.documentElement.setAttribute('data-theme', 'light');
            icon.textContent = '🌙'; // Button shows option to switch to Dark
        } else {
            // Auto/System
            document.documentElement.removeAttribute('data-theme');
            icon.textContent = systemDark.matches ? '☀️' : '🌙';
        }
    }

    // Initial Apply
    if (savedTheme) {
        applyTheme(savedTheme);
    } else {
        applyTheme('auto');
    }

    // Toggle Handler
    themeToggleBtn.addEventListener('click', () => {
        const current = document.documentElement.getAttribute('data-theme');
        let next = 'light';

        if (current === 'light') {
            next = 'dark';
        } else if (current === 'dark') {
            next = 'light'; // Or 'auto'? Let's stick to binary toggle for simplicity
        } else {
            // If auto, switch to opposite of system
            next = systemDark.matches ? 'light' : 'dark';
        }

        localStorage.setItem('theme', next);
        applyTheme(next);
    });

    // Listen for system changes (only if auto)
    systemDark.addEventListener('change', (e) => {
        if (!localStorage.getItem('theme')) {
            applyTheme('auto');
        }
    });
}
