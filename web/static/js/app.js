(function() {
    'use strict';

     
    const CHUNK_SIZE = 5 * 1024 * 1024;  
    const MAX_FILE_SIZE = window.CONFIG?.maxFileSize || 786432000;  
    const PARALLEL_CHUNK_UPLOADS = window.CONFIG?.parallelChunkUploads || 6;
    const MAX_CHUNK_UPLOAD_RETRIES = 5;

    let totalChunks = 0;
    let uploadedChunks = 0;

    let selectedFile = null;
    let encryptedBlob = null;
    let generatedPassword = null;
    let uploadSessionId = null;
    let pendingExpiresAt = null;
    let pendingCountdownTimer = null;
    let isUploading = false;
    let isFinalizing = false;
    let uploadComplete = false;
    let uploadError = null;
    let pendingAutoCopyText = null;
    let pendingAutoCopyBanner = false;
    let pendingAutoCopyBound = false;

     
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    const fileDetails = document.getElementById('file-details');
    const fileName = document.getElementById('file-name');
    const fileSize = document.getElementById('file-size');
    const resetVault = document.getElementById('reset-vault');
    const startOverBtn = document.getElementById('start-over-btn');
    const tosCheck = document.getElementById('tos-check');
    const finalizeBtn = document.getElementById('finalize-btn');
    const statusText = document.getElementById('status-text');
    const errorBanner = document.getElementById('error-banner');
    const errorBannerText = document.getElementById('error-banner-text');
    const errorBannerClose = document.getElementById('error-banner-close');

    const stageEntry = document.getElementById('stage-entry');
    const stageProcessing = document.getElementById('stage-processing');
    const stagePending = document.getElementById('stage-pending');
    const stageOutput = document.getElementById('stage-output');
    const pendingCountdown = document.getElementById('pending-countdown');
    
    const progressVal = document.getElementById('progress-val');
    const processMain = document.getElementById('process-main');
    const processSub = document.getElementById('process-sub');

    const outUrl = document.getElementById('out-url');
    const outPin = document.getElementById('out-pin');
    const outKey = document.getElementById('out-key');
    const outExpiryLabel = document.getElementById('out-expiry-label');

    async function init() {
         
        try {
            await SecureCrypto.loadWordList();
        } catch (error) {
            console.error('Failed to preload word list:', error);
        }

        setupEventListeners();
    }

    function setupEventListeners() {
         
        dropZone.addEventListener('click', () => fileInput.click());
        dropZone.addEventListener('dragover', handleDragOver);
        dropZone.addEventListener('dragleave', handleDragLeave);
        dropZone.addEventListener('drop', handleDrop);
        fileInput.addEventListener('change', handleFileSelect);

         
        resetVault.addEventListener('click', (e) => {
            e.stopPropagation();
            resetUpload();
        });
        startOverBtn.addEventListener('click', () => resetUpload());
        tosCheck.addEventListener('change', updateFinalizeButtonState);
        finalizeBtn.addEventListener('click', handleFinalize);

         
        document.querySelectorAll('.copy-trigger').forEach(btn => {
            btn.addEventListener('click', async function() {
                const input = this.parentElement.querySelector('input');
                const copied = await copyToClipboard(input.value);
                if (!copied) {
                    showToast('Copy failed. Please use Ctrl+C.');
                    return;
                }
                const original = this.innerHTML;
                this.innerHTML = '<i data-lucide="check" style="width: 1rem; height: 1rem;"></i>';
                this.style.background = 'var(--accent)';
                this.style.color = '#000';
                lucide.createIcons();
                setTimeout(() => {
                    this.innerHTML = original;
                    this.style.background = 'transparent';
                    this.style.color = 'inherit';
                    lucide.createIcons();
                }, 2000);
            });
        });

        document.querySelectorAll('input[name="expiration"]').forEach(input => {
            input.addEventListener('change', updateFinalizeButtonState);
        });

        if (errorBannerClose) {
            errorBannerClose.addEventListener('click', hideErrorBanner);
        }
    }

    function handleDragOver(e) {
        e.preventDefault();
        e.stopPropagation();
        e.dataTransfer.dropEffect = 'copy';
        dropZone.classList.add('active');
    }

    function handleDragLeave(e) {
        e.preventDefault();
        e.stopPropagation();
         
        if (e.target === dropZone) {
            dropZone.classList.remove('active');
        }
    }

    function handleDrop(e) {
        e.preventDefault();
        e.stopPropagation();
        dropZone.classList.remove('active');

        const files = e.dataTransfer.files;
        if (files.length > 0) {
            processFile(files[0]);
        }
    }

    function handleFileSelect(e) {
        const files = e.target.files;
        if (files.length > 0) {
            processFile(files[0]);
        }
    }

    async function processFile(file) {
        if (isUploading || isFinalizing) return;

        if (file.size > MAX_FILE_SIZE) {
            showErrorBanner(`File too large. Maximum size is ${SecureCrypto.formatFileSize(MAX_FILE_SIZE)}`);
            return;
        }
        if (file.size === 0) {
            showErrorBanner('Cannot upload empty file');
            return;
        }

        selectedFile = file;
        fileName.textContent = file.name;
        fileSize.textContent = SecureCrypto.formatFileSize(file.size);

        dropZone.classList.add('hidden');
        fileDetails.classList.remove('hidden');
        stageEntry.classList.add('hidden');
        stageProcessing.classList.add('hidden');
        stagePending.classList.remove('hidden');
        stageOutput.classList.add('hidden');
        statusText.textContent = 'Ready';
        statusText.style.color = 'var(--accent)';
        tosCheck.checked = false;
        updateFinalizeButtonState();

        runProtocolInBackground();
    }

    function handleFinalize() {
        if (!tosCheck.checked || isFinalizing) return;

        isFinalizing = true;
        updateFinalizeButtonState();
        stagePending.classList.add('hidden');
        stageProcessing.classList.remove('hidden');
        statusText.textContent = 'Uploading...';

        if (uploadComplete) {
            finalizeUpload();
        } else if (uploadError) {
            isFinalizing = false;
            updateFinalizeButtonState();
            stageProcessing.classList.add('hidden');
            stagePending.classList.remove('hidden');
            showErrorBanner('Upload failed: ' + uploadError);
        } else {
            const poll = setInterval(() => {
                if (uploadComplete) {
                    clearInterval(poll);
                    finalizeUpload();
                } else if (uploadError) {
                    clearInterval(poll);
                    isFinalizing = false;
                    updateFinalizeButtonState();
                    stageProcessing.classList.add('hidden');
                    stagePending.classList.remove('hidden');
                    statusText.textContent = 'Ready';
                    statusText.style.color = 'var(--accent)';
                    showErrorBanner('Upload failed: ' + uploadError);
                }
            }, 500);
        }
    }

    function updateFinalizeButtonState() {
        finalizeBtn.disabled = !tosCheck.checked || isFinalizing;
    }
    function updateUploadProgress() {
        if (totalChunks === 0) return;
        const pct = Math.floor((uploadedChunks / totalChunks) * 100);
        progressVal.textContent = `${pct}%`;
        processSub.textContent = pct < 30
            ? 'Sending your file...'
            : pct < 60
            ? 'Upload in progress...'
            : pct < 90
            ? 'Almost there...'
            : 'Finishing up...';
        processMain.textContent = 'Uploading';
    }

    async function runProtocolInBackground() {
        isUploading = true;
        uploadComplete = false;
        uploadError = null;

        try {
            generatedPassword = await SecureCrypto.generatePassword();
            encryptedBlob = await SecureCrypto.encryptFile(selectedFile, generatedPassword, () => {});
            await startUploadInBackground();
            uploadComplete = true;
            isUploading = false;
            updateFinalizeButtonState();
            if (!isFinalizing) {
                statusText.textContent = 'Ready';
                statusText.style.color = 'var(--accent)';
            }
        } catch (error) {
            console.error('Upload pipeline failed:', error);
            uploadError = error.message;
            isUploading = false;
            uploadComplete = false;
            isFinalizing = false;
            updateFinalizeButtonState();
            stageProcessing.classList.add('hidden');
            stagePending.classList.remove('hidden');
            statusText.textContent = 'Upload Failed';
            statusText.style.color = 'var(--color-error)';
            showErrorBanner('Upload failed: ' + error.message);
        }
    }

    async function waitForAssembly(sessionId, intervalMs = 1500, timeoutMs = 600000) {
        const deadline = Date.now() + timeoutMs;
        while (Date.now() < deadline) {
            const res = await fetch(`/api/upload/status/${sessionId}`);
            if (!res.ok) throw new Error('Failed to check assembly status');
            const { status } = await res.json();
            if (status === 'done') return;
            if (status.startsWith('error:')) throw new Error(status.slice(6));
            statusText.textContent = 'Finalizing...';
            await new Promise(r => setTimeout(r, intervalMs));
        }
        throw new Error('Assembly timed out');
    }

    async function runProtocol() {
        stageEntry.classList.add('hidden');
        stagePending.classList.add('hidden');
        stageOutput.classList.add('hidden');
        stageProcessing.classList.remove('hidden');
        statusText.textContent = 'Encrypting...';
        
        try {
             
            generatedPassword = await SecureCrypto.generatePassword();
            updateProgress(0, 'Scrambling data', 'Encrypting...');

             
            encryptedBlob = await SecureCrypto.encryptFile(
                selectedFile,
                generatedPassword,
                (progress, status) => {
                    updateProgress(progress * 0.5, status, 'Encrypting...');
                }
            );

            updateProgress(50, 'Up to the clouds', 'Uploading...');

             
            await startUpload();
            
        } catch (error) {
            console.error('Something failed:', error);
            showErrorBanner('Something failed: ' + error.message);
        }
    }

    async function startUploadInBackground() {
        if (!encryptedBlob) return;

        const initResponse = await initUpload();
        uploadSessionId = initResponse.session_id;
        totalChunks = initResponse.total_chunks;
        uploadedChunks = 0;

        await uploadChunksInBackground(initResponse);
        const completeResponse = await completeUpload();
        await waitForAssembly(uploadSessionId);

        pendingExpiresAt = completeResponse.pending_expires_at
            ? new Date(completeResponse.pending_expires_at).getTime()
            : null;
        startPendingCountdown();
    }

    async function startUpload() {
        if (isUploading || !encryptedBlob) return;

        isUploading = true;

        try {
             
            const initResponse = await initUpload();
            uploadSessionId = initResponse.session_id;

             
            await uploadChunks(initResponse);

             
            const completeResponse = await completeUpload();
            showPending(completeResponse);
            
        } catch (error) {
            console.error('Upload failed:', error);
            
             
            if (uploadSessionId) {
                try {
                    await cancelUpload();
                } catch (e) {
                    console.error('Failed to cancel upload:', e);
                }
            }

            showErrorBanner('Upload failed: ' + error.message);
            resetUpload();
        }

        isUploading = false;
    }

    async function uploadChunksInBackground(initResponse) {
        uploadedChunks = 0;
        await uploadChunksParallel(initResponse, () => {
            uploadedChunks++;
            updateUploadProgress();
        });
    }

    function getChunkBlob(chunkIndex) {
        const start = chunkIndex * CHUNK_SIZE;
        const end = Math.min(start + CHUNK_SIZE, encryptedBlob.size);
        return encryptedBlob.slice(start, end);
    }

    async function uploadChunkWithRetry(sessionId, chunkIndex) {
        const chunk = getChunkBlob(chunkIndex);
        let lastError;

        for (let attempt = 0; attempt < MAX_CHUNK_UPLOAD_RETRIES; attempt++) {
            if (attempt > 0) {
                await new Promise(r => setTimeout(r, 2000 * attempt));
            }

            try {
                const formData = new FormData();
                formData.append('session_id', sessionId);
                formData.append('chunk_index', chunkIndex.toString());
                formData.append('chunk', chunk);

                const response = await fetch('/api/upload/chunk', {
                    method: 'POST',
                    body: formData
                });

                if (!response.ok) {
                    const error = await response.json();
                    throw new Error(error.error || `Failed to upload chunk ${chunkIndex + 1}`);
                }

                return;
            } catch (error) {
                lastError = error;
                console.warn(`Chunk ${chunkIndex} attempt ${attempt + 1} failed:`, error.message);
            }
        }

        throw lastError;
    }

    async function uploadChunksParallel(initResponse, onChunkUploaded) {
        const totalChunks = initResponse.total_chunks;
        const concurrency = Math.max(1, Math.min(PARALLEL_CHUNK_UPLOADS, totalChunks));
        let nextChunkIndex = 0;

        const worker = async () => {
            while (true) {
                const chunkIndex = nextChunkIndex;
                nextChunkIndex++;

                if (chunkIndex >= totalChunks) {
                    return;
                }

                await uploadChunkWithRetry(initResponse.session_id, chunkIndex);
                if (onChunkUploaded) {
                    onChunkUploaded(chunkIndex, totalChunks);
                }
            }
        };

        const workers = Array.from({ length: concurrency }, () => worker());
        await Promise.all(workers);
    }

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
                chunk_size: CHUNK_SIZE
            })
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to initialize upload');
        }

        return response.json();
    }

    async function uploadChunks(initResponse) {
        const totalChunks = initResponse.total_chunks;
        let uploadedCount = 0;

        await uploadChunksParallel(initResponse, () => {
            uploadedCount++;
            const progress = 50 + (uploadedCount / totalChunks) * 45;
            updateProgress(progress, `Sending it high to the clouds`, 'Uploading...');
        });
    }

    async function completeUpload() {
        updateProgress(95, 'Making sure everything is okay', 'Finalizing...');

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

        updateProgress(100, 'Yippe', 'Complete!');
        return response.json();
    }

    function showPendingUI() {
        hideErrorBanner();
        stageEntry.classList.add('hidden');
        stageProcessing.classList.add('hidden');
        stagePending.classList.remove('hidden');
        stageOutput.classList.add('hidden');
        statusText.textContent = uploadError ? 'Upload Failed' : 'Pending Finalization';
        statusText.style.color = uploadError ? '#f44336' : 'var(--accent)';
        tosCheck.checked = false;
        updateFinalizeButtonState();
    }

    function showPending(response) {
        uploadSessionId = response.session_id;
        pendingExpiresAt = response.pending_expires_at ? new Date(response.pending_expires_at).getTime() : null;
        showPendingUI();
        startPendingCountdown();
    }

    function selectedDuration() {
        const checked = document.querySelector('input[name="expiration"]:checked');
        return checked ? checked.value : '24h';
    }

    function selectedDurationLabel() {
        const duration = selectedDuration();
        switch(duration) {
            case '24h': return '24 Hours';
            case '7d': return '7 Days';
            case '30d': return '30 Days';
            case '90d': return '3 Months';
            default: return '24 Hours';
        }
    }

    async function finalizeUpload() {
        statusText.textContent = 'Finalizing...';

        try {
            const response = await fetch('/api/upload/finalize', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    session_id: uploadSessionId,
                    duration: selectedDuration()
                })
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Failed to finalize upload');
            }

            const payload = await response.json();
            showSuccess(payload);
        } catch (error) {
            console.error('Finalize failed:', error);
            isFinalizing = false;
            updateFinalizeButtonState();
            stageProcessing.classList.add('hidden');
            stagePending.classList.remove('hidden');
            statusText.textContent = 'Ready';
            statusText.style.color = 'var(--accent)';
            showErrorBanner('Finalize failed: ' + error.message);
        }
    }

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

    function showSuccess(response) {
        clearPendingCountdown();
        isFinalizing = false;

        const fullShareUrl = `${response.share_url}#${generatedPassword}`;
        outUrl.value = fullShareUrl;
        outPin.value = response.numeric_code;
        outKey.value = generatedPassword;
        outExpiryLabel.textContent = `Expiry: ${selectedDurationLabel()} retention.`;
        uploadSessionId = null;

        attemptAutoCopy(fullShareUrl);

        stageProcessing.classList.add('hidden');
        stagePending.classList.add('hidden');
        stageOutput.classList.remove('hidden');
        statusText.textContent = 'Secure';
        statusText.style.color = 'var(--accent)';
    }

    function showErrorBanner(message) {
        if (!errorBanner) return;
        if (errorBannerText) {
            errorBannerText.textContent = message;
        }
        errorBanner.classList.remove('hidden');
    }

    function hideErrorBanner() {
        if (!errorBanner) return;
        errorBanner.classList.add('hidden');
    }

    function updateProgress(percent, sub, main) {
        progressVal.textContent = `${Math.floor(percent)}%`;
        if (sub) processSub.textContent = sub;
        if (main) processMain.textContent = main;
    }

    async function copyToClipboard(text, silent = false, showBanner = false) {
        try {
            await navigator.clipboard.writeText(text);
            if (!silent) {
                if (showBanner) {
                    showShareBanner();
                } else {
                    showToast('Copied to clipboard!');
                }
            }
            return true;
        } catch (error) {
            console.error('Failed to copy:', error);
            const textarea = document.createElement('textarea');
            textarea.value = text;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.select();
            const copiedWithFallback = document.execCommand('copy');
            document.body.removeChild(textarea);

            if (!copiedWithFallback) {
                return false;
            }

            if (!silent) {
                if (showBanner) {
                    showShareBanner();
                } else {
                    showToast('Copied to clipboard!');
                }
            }
            return true;
        }
    }

    async function attemptAutoCopy(text) {
        const copied = await copyToClipboard(text, true, true);
        if (copied) {
            showShareBanner();
            return;
        }

        showToast('Tap anywhere to retry copying the sharing link.');
        queueAutoCopyOnNextInteraction(text, true);
    }

    function queueAutoCopyOnNextInteraction(text, showBanner) {
        pendingAutoCopyText = text;
        pendingAutoCopyBanner = showBanner;

        if (pendingAutoCopyBound) {
            return;
        }

        pendingAutoCopyBound = true;
        ['click', 'keydown', 'touchstart'].forEach((eventName) => {
            document.addEventListener(eventName, handlePendingAutoCopy, true);
        });
    }

    async function handlePendingAutoCopy() {
        if (!pendingAutoCopyText) {
            clearPendingAutoCopyListeners();
            return;
        }

        const textToCopy = pendingAutoCopyText;
        const shouldShowBanner = pendingAutoCopyBanner;
        const copied = await copyToClipboard(textToCopy, true, shouldShowBanner);

        if (copied) {
            if (shouldShowBanner) {
                showShareBanner();
            } else {
                showToast('Copied to clipboard!');
            }
            pendingAutoCopyText = null;
            pendingAutoCopyBanner = false;
            clearPendingAutoCopyListeners();
        }
    }

    function clearPendingAutoCopyListeners() {
        if (!pendingAutoCopyBound) {
            return;
        }

        ['click', 'keydown', 'touchstart'].forEach((eventName) => {
            document.removeEventListener(eventName, handlePendingAutoCopy, true);
        });
        pendingAutoCopyBound = false;
    }

    function showShareBanner() {
        const banner = document.getElementById('share-banner');
        if (!banner) return;
        banner.classList.remove('hidden');
        banner.classList.add('visible');
        const closeBtn = document.getElementById('close-share-banner');
        if (closeBtn) {
            closeBtn.onclick = () => {
                banner.classList.remove('visible');
                setTimeout(() => banner.classList.add('hidden'), 350);
            };
        }
        setTimeout(() => {
            banner.classList.remove('visible');
            setTimeout(() => banner.classList.add('hidden'), 350);
        }, 3500);
    }

    function showToast(message) {
         
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
            background-color: #333;
            color: white;
            padding: 12px 24px;
            border-radius: 8px;
            z-index: 1000;
            animation: fadeIn 0.3s ease;
        `;

        document.body.appendChild(toast);

         
        setTimeout(() => {
            toast.style.animation = 'fadeOut 0.3s ease';
            setTimeout(() => toast.remove(), 300);
        }, 3000);
    }

    function resetUpload() {
        clearPendingCountdown();
        pendingAutoCopyText = null;
        pendingAutoCopyBanner = false;
        clearPendingAutoCopyListeners();
        hideErrorBanner();
        selectedFile = null;
        encryptedBlob = null;
        generatedPassword = null;
        const sessionToCancel = uploadSessionId;
        uploadSessionId = null;
        pendingExpiresAt = null;
        isFinalizing = false;
        isUploading = false;
        uploadComplete = false;
        uploadError = null;

         
        if (sessionToCancel) {
            fetch('/api/upload/cancel', {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ session_id: sessionToCancel })
            }).catch(e => console.error('Failed to cancel upload:', e));
        }

         
        fileInput.value = '';
        dropZone.classList.remove('hidden');
        fileDetails.classList.add('hidden');
        tosCheck.checked = false;
        finalizeBtn.disabled = true;
        statusText.textContent = 'Ready';
        statusText.style.color = 'var(--accent)';
        stageEntry.classList.remove('hidden');
        stageProcessing.classList.add('hidden');
        stagePending.classList.add('hidden');
        stageOutput.classList.add('hidden');
    }

    function startPendingCountdown() {
        clearPendingCountdown();
        if (!pendingExpiresAt) {
            pendingCountdown.textContent = '10:00';
            return;
        }

        const tick = () => {
            const remainingMs = pendingExpiresAt - Date.now();
            if (remainingMs <= 0) {
                clearPendingCountdown();
                pendingCountdown.textContent = '00:00';
                alert('Upload session expired. Please try again.');
                resetUpload();
                return;
            }

            const totalSeconds = Math.floor(remainingMs / 1000);
            const minutes = Math.floor(totalSeconds / 60);
            const seconds = totalSeconds % 60;
            pendingCountdown.textContent = `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
        };

        tick();
        pendingCountdownTimer = setInterval(tick, 1000);
    }

    function clearPendingCountdown() {
        if (pendingCountdownTimer) {
            clearInterval(pendingCountdownTimer);
            pendingCountdownTimer = null;
        }
    }

     
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

     
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();