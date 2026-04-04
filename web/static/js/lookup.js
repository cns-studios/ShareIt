(function() {
    'use strict';

     
    let fileMetadata = null;
    let encryptedBlob = null;
    let currentPassword = null;

     
    const lookupSection = document.getElementById('lookup-section');
    const loadingSection = document.getElementById('loading-section');
    const resultSection = document.getElementById('result-section');
    const successSection = document.getElementById('success-section');
    const errorSection = document.getElementById('error-section');

    const codeInput = document.getElementById('code-input');
    const lookupBtn = document.getElementById('lookup-btn');

    const fileNameEl = document.getElementById('file-name');
    const fileSizeEl = document.getElementById('file-size');
    const fileCreatedEl = document.getElementById('file-created');
    const fileExpiresEl = document.getElementById('file-expires');

    const passwordInput = document.getElementById('password-input');
    const downloadBtn = document.getElementById('download-btn');
    const progressSection = document.getElementById('progress-section');
    const progressFill = document.getElementById('progress-fill');
    const progressText = document.getElementById('progress-text');

    const searchAnotherBtn = document.getElementById('search-another');
    const downloadAgainBtn = document.getElementById('download-again-btn');
    const searchNewBtn = document.getElementById('search-new-btn');
    const tryAgainBtn = document.getElementById('try-again-btn');

    const errorTitle = document.getElementById('error-title');
    const errorMessage = document.getElementById('error-message');

    function init() {
        setupEventListeners();
        codeInput.focus();
    }

    function setupEventListeners() {
         
        lookupBtn.addEventListener('click', lookupFile);

         
        codeInput.addEventListener('input', (e) => {
            e.target.value = e.target.value.replace(/\D/g, '').slice(0, 12);
        });

         
        codeInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter' && codeInput.value.length === 12) {
                lookupFile();
            }
        });

         
        downloadBtn.addEventListener('click', () => {
            const password = passwordInput.value.trim().toLowerCase();
            downloadAndDecrypt(password);
        });

         
        passwordInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') {
                downloadBtn.click();
            }
        });

         
        if (searchAnotherBtn) searchAnotherBtn.addEventListener('click', resetToLookup);
        if (searchNewBtn) searchNewBtn.addEventListener('click', resetToLookup);
        if (tryAgainBtn) tryAgainBtn.addEventListener('click', handleTryAgain);
        if (downloadAgainBtn) downloadAgainBtn.addEventListener('click', () => downloadAndDecrypt(currentPassword));
    }

    async function lookupFile() {
        const code = codeInput.value.trim();

         
        if (code.length !== 12) {
            showError('Invalid Code', 'Please enter a 12-digit numeric code.');
            return;
        }

        if (!/^\d{12}$/.test(code)) {
            showError('Invalid Code', 'Code must contain only digits.');
            return;
        }

         
        lookupSection.classList.add('hidden');
        errorSection.classList.add('hidden');
        loadingSection.classList.remove('hidden');

        try {
            const response = await fetch(`/api/file/code/${code}`);

            if (!response.ok) {
                const error = await response.json();
                handleAPIError(error);
                return;
            }

            fileMetadata = await response.json();
            displayFileMetadata();

            loadingSection.classList.add('hidden');
            resultSection.classList.remove('hidden');
            passwordInput.focus();

        } catch (error) {
            console.error('Lookup failed:', error);
            showError('Connection Error', 'Failed to connect to server. Please try again.');
        }
    }

    function displayFileMetadata() {
        fileNameEl.textContent = fileMetadata.original_name;
        fileSizeEl.textContent = SecureCrypto.formatFileSize(fileMetadata.size_bytes);
        fileCreatedEl.textContent = SecureCrypto.formatDate(fileMetadata.created_at);
        fileExpiresEl.textContent = SecureCrypto.getTimeRemaining(fileMetadata.expires_at);
    }

    async function downloadAndDecrypt(password) {
         
        const validation = SecureCrypto.validatePassword(password);
        if (!validation.valid) {
            showError('Invalid Passcode', validation.error);
            return;
        }

        currentPassword = password;

         
        progressSection.classList.remove('hidden');
        downloadBtn.disabled = true;
        passwordInput.disabled = true;

        try {
             
            updateProgress(0, 'Downloading file...');
            encryptedBlob = await downloadEncryptedFile();

             
            updateProgress(50, 'Decrypting file...');
            const decryptedData = await SecureCrypto.decryptBlob(
                encryptedBlob,
                password,
                (progress, status) => {
                    updateProgress(50 + (progress * 0.4), status);
                }
            );

             
            updateProgress(95, 'Preparing download...');
            triggerDownload(decryptedData, fileMetadata.original_name);

            updateProgress(100, 'Complete!');

             
            resultSection.classList.add('hidden');
            successSection.classList.remove('hidden');

        } catch (error) {
            console.error('Something failed:', error);
            progressSection.classList.add('hidden');
            downloadBtn.disabled = false;
            passwordInput.disabled = false;

            if (error.message.includes('Decryption failed')) {
                showInlineError('Invalid passcode. Please check and try again.');
            } else {
                showError('Download Failed', error.message);
            }
        }
    }

    async function downloadEncryptedFile() {
        const response = await fetch(`/api/file/${fileMetadata.id}/download`);

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to download file');
        }

        const contentLength = response.headers.get('Content-Length');
        const total = parseInt(contentLength, 10);
        const reader = response.body.getReader();
        const chunks = [];
        let received = 0;

        while (true) {
            const { done, value } = await reader.read();

            if (done) break;

            chunks.push(value);
            received += value.length;

            if (total) {
                const progress = (received / total) * 50;
                updateProgress(progress, `Downloading... ${SecureCrypto.formatFileSize(received)} / ${SecureCrypto.formatFileSize(total)}`);
            }
        }

        return new Blob(chunks);
    }

    function triggerDownload(data, filename) {
        const blob = new Blob([data], { type: 'application/octet-stream' });
        const url = URL.createObjectURL(blob);

        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        a.style.display = 'none';

        document.body.appendChild(a);
        a.click();

        setTimeout(() => {
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        }, 100);
    }

    function handleAPIError(error) {
        loadingSection.classList.add('hidden');

        switch (error.code) {
            case 'FILE_NOT_FOUND':
                showError('File Not Found', 'No file found with this code. Please check and try again.');
                break;
            case 'FILE_EXPIRED':
                showError('File Expired', 'This file has expired and is no longer available.');
                break;
            case 'FILE_DELETED':
                showError('File Removed', 'This file has been removed due to policy violations.');
                break;
            case 'INVALID_CODE_FORMAT':
                showError('Invalid Code', 'Please enter a valid 12-digit numeric code.');
                break;
            default:
                showError('Error', error.error || 'An unexpected error occurred.');
        }
    }

    function showError(title, message) {
        errorTitle.textContent = title;
        errorMessage.textContent = message;

        lookupSection.classList.add('hidden');
        loadingSection.classList.add('hidden');
        resultSection.classList.add('hidden');
        successSection.classList.add('hidden');
        errorSection.classList.remove('hidden');
    }

    function showInlineError(message) {
        const existingToast = document.querySelector('.toast');
        if (existingToast) {
            existingToast.remove();
        }

        const toast = document.createElement('div');
        toast.className = 'toast';
        toast.textContent = message;
        toast.style.cssText = `
            position: fixed;
            bottom: 20px;
            left: 50%;
            transform: translateX(-50%);
            background-color: #f44336;
            color: white;
            padding: 12px 24px;
            border-radius: 8px;
            z-index: 1001;
            max-width: 90%;
            text-align: center;
        `;

        document.body.appendChild(toast);

        setTimeout(() => {
            toast.remove();
        }, 4000);
    }

    function resetToLookup() {
        fileMetadata = null;
        encryptedBlob = null;
        currentPassword = null;

        codeInput.value = '';
        passwordInput.value = '';
        progressSection.classList.add('hidden');
        downloadBtn.disabled = false;
        passwordInput.disabled = false;
        updateProgress(0, '');

        resultSection.classList.add('hidden');
        successSection.classList.add('hidden');
        errorSection.classList.add('hidden');
        loadingSection.classList.add('hidden');
        lookupSection.classList.remove('hidden');

        codeInput.focus();
    }

    function handleTryAgain() {
        errorSection.classList.add('hidden');
        
         
        if (fileMetadata) {
            resultSection.classList.remove('hidden');
            passwordInput.value = '';
            passwordInput.focus();
        } else {
             
            lookupSection.classList.remove('hidden');
            codeInput.focus();
        }
    }

    function updateProgress(percent, text) {
        progressFill.style.width = `${percent}%`;
        if (text) {
            progressText.textContent = text;
        }
    }

     
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();