const SecureCrypto = (function() {
    'use strict';

     
    const CONFIG = {
        algorithm: 'AES-GCM',
        keyLength: 256,
        ivLength: 12,
        saltLength: 16,
        pbkdf2Iterations: 100000,
        wordCount: 5
    };

     
    let wordList = null;

    async function loadWordList() {
        if (wordList) return wordList;
        
        try {
            const response = await fetch('/static/wordlist.txt');
            const text = await response.text();
            wordList = text.trim().split('\n').map(w => w.trim().toLowerCase());
            console.log(`Loaded ${wordList.length} words`);
            return wordList;
        } catch (error) {
            console.error('Failed to load word list:', error);
            throw new Error('Failed to load word list');
        }
    }

    async function generatePassword() {
        const words = await loadWordList();
        const selectedWords = [];
        const randomValues = new Uint32Array(CONFIG.wordCount);
        crypto.getRandomValues(randomValues);

        for (let i = 0; i < CONFIG.wordCount; i++) {
            const index = randomValues[i] % words.length;
            selectedWords.push(words[index]);
        }

        return selectedWords.join('-');
    }

    async function deriveKey(password, salt) {
        const encoder = new TextEncoder();
        const passwordBuffer = encoder.encode(password);

         
        const keyMaterial = await crypto.subtle.importKey(
            'raw',
            passwordBuffer,
            'PBKDF2',
            false,
            ['deriveKey']
        );

         
        const key = await crypto.subtle.deriveKey(
            {
                name: 'PBKDF2',
                salt: salt,
                iterations: CONFIG.pbkdf2Iterations,
                hash: 'SHA-256'
            },
            keyMaterial,
            {
                name: CONFIG.algorithm,
                length: CONFIG.keyLength
            },
            false,
            ['encrypt', 'decrypt']
        );

        return key;
    }


    function generateRandomBytes(length) {
        const bytes = new Uint8Array(length);
        crypto.getRandomValues(bytes);
        return bytes;
    }


    async function encrypt(data, password) {
        const salt = generateRandomBytes(CONFIG.saltLength);
        const iv = generateRandomBytes(CONFIG.ivLength);
        const key = await deriveKey(password, salt);

        const ciphertext = await crypto.subtle.encrypt(
            {
                name: CONFIG.algorithm,
                iv: iv
            },
            key,
            data
        );

         
        const result = new Uint8Array(salt.length + iv.length + ciphertext.byteLength);
        result.set(salt, 0);
        result.set(iv, salt.length);
        result.set(new Uint8Array(ciphertext), salt.length + iv.length);

        return result;
    }


    async function decrypt(encryptedData, password) {
        const data = new Uint8Array(encryptedData);

         
        const salt = data.slice(0, CONFIG.saltLength);
        const iv = data.slice(CONFIG.saltLength, CONFIG.saltLength + CONFIG.ivLength);
        const ciphertext = data.slice(CONFIG.saltLength + CONFIG.ivLength);

        const key = await deriveKey(password, salt);

        try {
            const decrypted = await crypto.subtle.decrypt(
                {
                    name: CONFIG.algorithm,
                    iv: iv
                },
                key,
                ciphertext
            );

            return new Uint8Array(decrypted);
        } catch (error) {
            throw new Error('Decryption failed. Invalid password or corrupted data.');
        }
    }

    async function encryptFile(file, password, onProgress) {
        return new Promise((resolve, reject) => {
            const reader = new FileReader();

            reader.onload = async function(e) {
                try {
                    if (onProgress) onProgress(0, 'Encrypting...');
                    
                    const data = new Uint8Array(e.target.result);
                    const encrypted = await encrypt(data, password);
                    
                    if (onProgress) onProgress(100, 'Encryption complete');
                    
                    resolve(new Blob([encrypted], { type: 'application/octet-stream' }));
                } catch (error) {
                    reject(error);
                }
            };

            reader.onerror = function() {
                reject(new Error('Failed to read file'));
            };

            reader.readAsArrayBuffer(file);
        });
    }

    async function decryptBlob(blob, password, onProgress) {
        return new Promise((resolve, reject) => {
            const reader = new FileReader();

            reader.onload = async function(e) {
                try {
                    if (onProgress) onProgress(0, 'Decrypting...');
                    
                    const data = new Uint8Array(e.target.result);
                    const decrypted = await decrypt(data, password);
                    
                    if (onProgress) onProgress(100, 'Decryption complete');
                    
                    resolve(decrypted);
                } catch (error) {
                    reject(error);
                }
            };

            reader.onerror = function() {
                reject(new Error('Failed to read encrypted data'));
            };

            reader.readAsArrayBuffer(blob);
        });
    }

    function validatePassword(password) {
        if (!password || typeof password !== 'string') {
            return { valid: false, error: 'Password is required' };
        }

        const words = password.toLowerCase().trim().split('-');
        
        if (words.length !== CONFIG.wordCount) {
            return { 
                valid: false, 
                error: `Password must contain exactly ${CONFIG.wordCount} words separated by hyphens` 
            };
        }

        for (const word of words) {
            if (!/^[a-z]+$/.test(word)) {
                return { 
                    valid: false, 
                    error: 'Password words must contain only letters' 
                };
            }
            if (word.length < 2) {
                return { 
                    valid: false, 
                    error: 'Each word must be at least 2 characters' 
                };
            }
        }

        return { valid: true };
    }

    function getPasswordFromHash() {
        const hash = window.location.hash;
        if (!hash || hash.length <= 1) {
            return null;
        }
        return decodeURIComponent(hash.substring(1));
    }

    function formatFileSize(bytes) {
        if (bytes === 0) return '0 Bytes';
        
        const k = 1024;
        const sizes = ['Bytes', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    function formatDate(dateString) {
        const date = new Date(dateString);
        return date.toLocaleString();
    }

    function getTimeRemaining(expiresAt) {
        const now = new Date();
        const expires = new Date(expiresAt);
        const diff = expires - now;

        if (diff <= 0) {
            return 'Expired';
        }

        const days = Math.floor(diff / (1000 * 60 * 60 * 24));
        const hours = Math.floor((diff % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
        const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));

        if (days > 0) {
            return `${days}d ${hours}h remaining`;
        } else if (hours > 0) {
            return `${hours}h ${minutes}m remaining`;
        } else {
            return `${minutes}m remaining`;
        }
    }

     
    return {
        generatePassword,
        encryptFile,
        decryptBlob,
        validatePassword,
        getPasswordFromHash,
        formatFileSize,
        formatDate,
        getTimeRemaining,
        loadWordList
    };
})();

 
window.SecureCrypto = SecureCrypto;