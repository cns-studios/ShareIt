/**
 * SecureShare Upload Application
 * Handles file upload with client-side encryption
 */

(function() {
    'use strict';

    // Configuration
    const CHUNK_SIZE = 5 * 1024 * 1024; // 5MB chunks
    const MAX_FILE_SIZE = window.CONFIG?.maxFileSize || 786432000; // 750MB

    // State
    let selectedFile = null;
    let encryptedBlob = null;
    let generatedPassword = null;
    let uploadSessionId = null;
    let isUploading = false;
    let isEncrypting = false;

    // DOM Elements
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    const fileInfo = document.getElementById('file-info');
    const fileName = document.getElementById('file-name');
    const fileSize = document.getElementById('file-size');
    const removeFileBtn = document.getElementById('remove-file');
    const progressSection = document.getElementById('progress-section');
    const progressFill = document.getElementById('progress-fill');
    const progressText = document.getElementById('progress-text');
    const optionsSection = document.getElementById('options-section');
    const durationSelect = document.getElementById('duration');
    const agreeTosCheckbox = document.getElementById('agree-tos');
    const uploadBtn = document.getElementById('upload-btn');
    const uploadSection = document.getElementById('upload-section');
    const resultSection = document.getElementById('result-section');
    const errorSection = document.getElementById('error-section');
    const shareLink = document.getElementById('share-link');
    const numericCode = document.getElementById('numeric-code');
    const passwordWords = document.getElementById('password-words');
    const copyLinkBtn = document.getElementById('copy-link');
    const copyCodeBtn = document.getElementById('copy-code');
    const copyPasswordBtn = document.getElementById('copy-password');
    const uploadAnotherBtn = document.getElementById('upload-another');
    const tryAgainBtn = document.getElementById('try-again');
    const errorMessage = document.getElementById('error-message');

    /**
     * Initialize the application
     */
    async function init() {
        // Preload word list
        try {
            await SecureCrypto.loadWordList();
        } catch (error) {
            console.error('Failed to preload word list:', error);
        }

        setupEventListeners();
    }

    /**
     * Set up event listeners
     */
    function setupEventListeners() {
        // Drop zone events
        dropZone.addEventListener('click', () => fileInput.click());
        dropZone.addEventListener('dragover', handleDragOver);
        dropZone.addEventListener('dragleave', handleDragLeave);
        dropZone.addEventListener('drop', handleDrop);
        fileInput.addEventListener('change', handleFileSelect);

        // Remove file button
        removeFileBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            resetUpload();
        });

        // ToS checkbox
        agreeTosCheckbox.addEventListener('change', updateUploadButtonState);

        // Upload button
        uploadBtn.addEventListener('click', handleUpload);

        // Copy buttons
        copyLinkBtn.addEventListener('click', () => copyToClipboard(shareLink.value, 'Link'));
        copyCodeBtn.addEventListener('click', () => copyToClipboard(numericCode.value, 'Code'));
        copyPasswordBtn.addEventListener('click', () => copyToClipboard(passwordWords.value, 'Password'));

        // Reset buttons
        uploadAnotherBtn.addEventListener('click', resetAll);
        tryAgainBtn.addEventListener('click', resetAll);
    }

    /**
     * Handle drag over event
     */
    function handleDragOver(e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.add('drag-over');
    }

    /**
     * Handle drag leave event
     */
    function handleDragLeave(e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.remove('drag-over');
    }

    /**
     * Handle drop event
     */
    function handleDrop(e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.remove('drag-over');

        const files = e.dataTransfer.files;
        if (files.length > 0) {
            processFile(files[0]);
        }
    }

    /**
     * Handle file input change
     */
    function handleFileSelect(e) {
        const files = e.target.files;
        if (files.length > 0) {
            processFile(files[0]);
        }
    }

    /**
     * Process selected file
     */
    async function processFile(file) {
        // Validate file size
        if (file.size > MAX_FILE_SIZE) {
            showError(`File too large. Maximum size is ${SecureCrypto.formatFileSize(MAX_FILE_SIZE)}`);
            return;
        }

        if (file.size === 0) {
            showError('Cannot upload empty file');
            return;
        }

        selectedFile = file;

        // Update UI
        fileName.textContent = file.name;
        fileSize.textContent = SecureCrypto.formatFileSize(file.size);
        dropZone.classList.add('hidden');
        fileInfo.classList.remove('hidden');
        optionsSection.classList.remove('hidden');
        progressSection.classList.remove('hidden');

        // Start encryption immediately
        await startEncryption();
    }

    /**
     * Start file encryption
     */
    async function startEncryption() {
        if (isEncrypting || !selectedFile) return;

        isEncrypting = true;
        updateProgress(0, 'Generating encryption key...');

        try {
            // Generate password
            generatedPassword = await SecureCrypto.generatePassword();
            updateProgress(10, 'Encrypting file...');

            // Encrypt file
            encryptedBlob = await SecureCrypto.encryptFile(
                selectedFile,
                generatedPassword,
                (progress, status) => {
                    updateProgress(10 + (progress * 0.4), status);
                }
            );

            updateProgress(50, 'Encryption complete. Ready to upload.');
            isEncrypting = false;

            // Auto-start upload if ToS is checked
            if (agreeTosCheckbox.checked) {
                startUpload();
            }
        } catch (error) {
            console.error('Encryption failed:', error);
            showError('Encryption failed: ' + error.message);
            isEncrypting = false;
        }
    }

    /**
     * Update upload button state
     */
    function updateUploadButtonState() {
        uploadBtn.disabled = !agreeTosCheckbox.checked || isEncrypting || !encryptedBlob;
        
        // Auto-start upload if encryption is complete and ToS is checked
        if (agreeTosCheckbox.checked && encryptedBlob && !isUploading) {
            startUpload();
        }
    }

    /**
     * Handle upload button click
     */
    function handleUpload() {
        if (!encryptedBlob || isUploading) return;
        startUpload();
    }

    /**
     * Start the upload process
     */
    async function startUpload() {
        if (isUploading || !encryptedBlob) return;

        isUploading = true;
        uploadBtn.disabled = true;
        updateProgress(50, 'Starting upload...');

        try {
            // Initialize upload session
            const initResponse = await initUpload();
            uploadSessionId = initResponse.session_id;

            // Upload chunks
            await uploadChunks(initResponse);

            // Complete upload
            const completeResponse = await completeUpload();

            // Show success
            showSuccess(completeResponse);
        } catch (error) {
            console.error('Upload failed:', error);
            
            // Cancel the upload session if it exists
            if (uploadSessionId) {
                try {
                    await cancelUpload();
                } catch (e) {
                    console.error('Failed to cancel upload:', e);
                }
            }

            showError('Upload failed: ' + error.message);
        } finally {
            isUploading = false;
        }
    }

    /**
     * Initialize upload session
     */
    async function initUpload() {
        const totalChunks = Math.ceil(encryptedBlob.size / CHUNK_SIZE);

        const response = await fetch('/api/upload/init', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                file_name: selectedFile.name,
                file_size: encryptedBlob.size,
                total_chunks: totalChunks,
                chunk_size: CHUNK_SIZE,
                duration: durationSelect.value
            })
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to initialize upload');
        }

        return response.json();
    }

    /**
     * Upload file chunks
     */
    async function uploadChunks(initResponse) {
        const totalChunks = initResponse.total_chunks;
        let uploadedChunks = 0;

        for (let i = 0; i < totalChunks; i++) {
            const start = i * CHUNK_SIZE;
            const end = Math.min(start + CHUNK_SIZE, encryptedBlob.size);
            const chunk = encryptedBlob.slice(start, end);

            const formData = new FormData();
            formData.append('session_id', initResponse.session_id);
            formData.append('chunk_index', i.toString());
            formData.append('chunk', chunk);

            const response = await fetch('/api/upload/chunk', {
                method: 'POST',
                body: formData
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `Failed to upload chunk ${i + 1}`);
            }

            uploadedChunks++;
            const progress = 50 + (uploadedChunks / totalChunks) * 45;
            updateProgress(progress, `Uploading... ${uploadedChunks}/${totalChunks} chunks`);
        }
    }

    /**
     * Complete the upload
     */
    async function completeUpload() {
        updateProgress(95, 'Finalizing upload...');

        const response = await fetch('/api/upload/complete', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                session_id: uploadSessionId,
                confirmed: true
            })
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to complete upload');
        }

        updateProgress(100, 'Upload complete!');
        return response.json();
    }

    /**
     * Cancel upload session
     */
    async function cancelUpload() {
        if (!uploadSessionId) return;

        await fetch('/api/upload/cancel', {
            method: 'DELETE',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                session_id: uploadSessionId
            })
        });

        uploadSessionId = null;
    }

    /**
     * Show success result
     */
    function showSuccess(response) {
        // Build share URL with password
        const fullShareUrl = `${response.share_url}#${generatedPassword}`;

        // Update UI
        shareLink.value = fullShareUrl;
        numericCode.value = response.numeric_code;
        passwordWords.value = generatedPassword;

        // Copy link to clipboard automatically
        copyToClipboard(fullShareUrl, 'Link', true);

        // Show result section
        uploadSection.classList.add('hidden');
        resultSection.classList.remove('hidden');
    }

    /**
     * Show error
     */
    function showError(message) {
        errorMessage.textContent = message;
        uploadSection.classList.add('hidden');
        resultSection.classList.add('hidden');
        errorSection.classList.remove('hidden');
    }

    /**
     * Update progress bar
     */
    function updateProgress(percent, text) {
        progressFill.style.width = `${percent}%`;
        if (text) {
            progressText.textContent = text;
        }
    }

    /**
     * Copy text to clipboard
     */
    async function copyToClipboard(text, label, silent = false) {
        try {
            await navigator.clipboard.writeText(text);
            if (!silent) {
                showToast(`${label} copied to clipboard!`);
            }
        } catch (error) {
            console.error('Failed to copy:', error);
            // Fallback for older browsers
            const textarea = document.createElement('textarea');
            textarea.value = text;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.select();
            document.execCommand('copy');
            document.body.removeChild(textarea);
            if (!silent) {
                showToast(`${label} copied to clipboard!`);
            }
        }
    }

    /**
     * Show toast notification
     */
    function showToast(message) {
        // Remove existing toast
        const existingToast = document.querySelector('.toast');
        if (existingToast) {
            existingToast.remove();
        }

        // Create toast
        const toast = document.createElement('div');
        toast.className = 'toast';
        toast.textContent = message;
        toast.style.cssText = `
            position: fixed;
            bottom: 20px;
            left: 50%;
            transform: translateX(-50%);
            background-color: #333;
            color: white;
            padding: 12px 24px;
            border-radius: 8px;
            z-index: 1000;
            animation: fadeIn 0.3s ease;
        `;

        document.body.appendChild(toast);

        // Remove after 3 seconds
        setTimeout(() => {
            toast.style.animation = 'fadeOut 0.3s ease';
            setTimeout(() => toast.remove(), 300);
        }, 3000);
    }

    /**
     * Reset upload state
     */
    function resetUpload() {
        selectedFile = null;
        encryptedBlob = null;
        generatedPassword = null;
        isEncrypting = false;

        // Cancel any ongoing upload
        if (uploadSessionId) {
            cancelUpload();
        }

        // Reset UI
        fileInput.value = '';
        dropZone.classList.remove('hidden');
        fileInfo.classList.add('hidden');
        optionsSection.classList.add('hidden');
        progressSection.classList.add('hidden');
        updateProgress(0, '');
    }

    /**
     * Reset everything
     */
    function resetAll() {
        resetUpload();
        agreeTosCheckbox.checked = false;
        uploadBtn.disabled = true;
        uploadSection.classList.remove('hidden');
        resultSection.classList.add('hidden');
        errorSection.classList.add('hidden');
    }

    // Add CSS for toast animations
    const style = document.createElement('style');
    style.textContent = `
        @keyframes fadeIn {
            from { opacity: 0; transform: translateX(-50%) translateY(20px); }
            to { opacity: 1; transform: translateX(-50%) translateY(0); }
        }
        @keyframes fadeOut {
            from { opacity: 1; transform: translateX(-50%) translateY(0); }
            to { opacity: 0; transform: translateX(-50%) translateY(20px); }
        }
    `;
    document.head.appendChild(style);

    // Initialize on DOM ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();