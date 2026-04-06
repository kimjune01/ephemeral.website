const API = window.API_URL || '/api';

// Route: "/" = upload, anything else = player
const path = window.location.pathname.replace(/^\/+|\/+$/g, '');

if (path === '') {
    initUpload();
} else if (path === 'how') {
    document.getElementById('view-how').hidden = false;
} else {
    initPlayer(path);
}

// ── UPLOAD ──────────────────────────────────────────────

function initUpload() {
    document.getElementById('view-upload').hidden = false;

    const uploadArea = document.getElementById('upload-area');
    const recordArea = document.getElementById('record-area');
    const tabUpload = document.getElementById('tab-upload');
    const tabRecord = document.getElementById('tab-record');
    const fileInput = document.getElementById('file-input');
    const uploadPrompt = document.getElementById('upload-prompt');
    const noteArea = document.getElementById('note-area');
    const slugInput = document.getElementById('slug-input');
    const slugError = document.getElementById('slug-error');
    const noteInput = document.getElementById('note-input');
    const charCount = document.getElementById('char-count');
    const sendBtn = document.getElementById('send-btn');
    const uploadProgress = document.getElementById('upload-progress');
    const progressFill = document.getElementById('upload-progress-fill');
    const result = document.getElementById('result');
    const linkOutput = document.getElementById('link-output');
    const copyBtn = document.getElementById('copy-btn');

    // Record elements
    const recordBtn = document.getElementById('record-btn');
    const recordIcon = document.getElementById('record-icon');
    const recordStatus = document.getElementById('record-status');
    const recordTime = document.getElementById('record-time');

    let selectedFile = null;
    let mediaRecorder = null;
    let recordedChunks = [];
    let recordTimer = null;
    let recordSeconds = 0;

    // Tab switching
    tabUpload.addEventListener('click', () => {
        tabUpload.classList.add('active');
        tabRecord.classList.remove('active');
        uploadArea.hidden = false;
        recordArea.hidden = true;
    });

    tabRecord.addEventListener('click', () => {
        tabRecord.classList.add('active');
        tabUpload.classList.remove('active');
        uploadArea.hidden = true;
        recordArea.hidden = false;
    });

    // Recording
    recordBtn.addEventListener('click', async () => {
        if (mediaRecorder && mediaRecorder.state === 'recording') {
            // Stop
            mediaRecorder.stop();
            recordBtn.classList.remove('recording');
            recordStatus.textContent = 'processing...';
            clearInterval(recordTimer);
            return;
        }

        try {
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            recordedChunks = [];
            recordSeconds = 0;
            mediaRecorder = new MediaRecorder(stream);

            mediaRecorder.ondataavailable = (e) => {
                if (e.data.size > 0) recordedChunks.push(e.data);
            };

            mediaRecorder.onstop = () => {
                stream.getTracks().forEach(t => t.stop());
                const blob = new Blob(recordedChunks, { type: 'audio/webm' });

                if (blob.size > 5 * 1024 * 1024) {
                    recordStatus.textContent = 'too long (max 5 MB)';
                    recordTime.hidden = true;
                    return;
                }

                selectedFile = new File([blob], 'recording.webm', { type: 'audio/webm' });
                recordArea.hidden = true;
                uploadArea.hidden = true;
                tabUpload.classList.remove('active');
                tabRecord.classList.remove('active');
                noteArea.hidden = false;
                slugInput.focus();
            };

            mediaRecorder.start();
            recordBtn.classList.add('recording');
            recordStatus.textContent = 'recording';
            recordTime.hidden = false;
            recordTime.textContent = '0:00';

            recordTimer = setInterval(() => {
                recordSeconds++;
                const m = Math.floor(recordSeconds / 60);
                const s = recordSeconds % 60;
                recordTime.textContent = `${m}:${s.toString().padStart(2, '0')}`;
            }, 1000);
        } catch {
            recordStatus.textContent = 'microphone access denied';
        }
    });

    uploadArea.addEventListener('click', () => {
        if (!selectedFile) fileInput.click();
    });

    uploadArea.addEventListener('dragover', (e) => {
        e.preventDefault();
        uploadArea.classList.add('dragover');
    });

    uploadArea.addEventListener('dragleave', () => {
        uploadArea.classList.remove('dragover');
    });

    uploadArea.addEventListener('drop', (e) => {
        e.preventDefault();
        uploadArea.classList.remove('dragover');
        const file = e.dataTransfer.files[0];
        if (file) selectFile(file);
    });

    fileInput.addEventListener('change', () => {
        if (fileInput.files[0]) selectFile(fileInput.files[0]);
    });

    noteInput.addEventListener('input', () => {
        charCount.textContent = `${noteInput.value.length}/280`;
    });

    slugInput.addEventListener('input', () => {
        slugError.hidden = true;
        slugInput.value = slugInput.value.toLowerCase().replace(/[^a-z0-9\-]/g, '');
    });

    sendBtn.addEventListener('click', () => upload());

    copyBtn.addEventListener('click', () => {
        linkOutput.select();
        navigator.clipboard.writeText(linkOutput.value);
        copyBtn.textContent = 'Copied';
        setTimeout(() => copyBtn.textContent = 'Copy', 2000);
    });

    function selectFile(file) {
        if (!file.type.startsWith('audio/')) {
            alert('Please select an audio file.');
            return;
        }
        if (file.size > 5 * 1024 * 1024) {
            alert('File too large (max 5 MB).');
            return;
        }
        selectedFile = file;
        uploadPrompt.hidden = true;
        noteArea.hidden = false;
        uploadArea.style.cursor = 'default';
        slugInput.focus();
    }

    async function upload() {
        if (!selectedFile) return;

        slugError.hidden = true;
        noteArea.hidden = true;
        uploadProgress.hidden = false;
        sendBtn.disabled = true;
        progressFill.style.width = '20%';

        try {
            const resp = await fetch(`${API}/upload`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    slug: slugInput.value.trim() || undefined,
                    note: noteInput.value.trim() || undefined,
                    content_type: selectedFile.type,
                }),
            });

            if (resp.status === 409) {
                slugError.hidden = false;
                slugInput.focus();
                slugInput.select();
                noteArea.hidden = false;
                uploadProgress.hidden = true;
                progressFill.style.width = '0%';
                sendBtn.disabled = false;
                return;
            }

            if (!resp.ok) {
                const err = await resp.json().catch(() => ({}));
                throw new Error(err.error || 'Upload failed');
            }

            const data = await resp.json();
            progressFill.style.width = '40%';

            const s3Resp = await fetch(data.upload_url, {
                method: 'PUT',
                headers: { 'Content-Type': selectedFile.type },
                body: selectedFile,
            });

            if (!s3Resp.ok) {
                throw new Error('Failed to upload audio');
            }

            progressFill.style.width = '100%';

            const url = `${window.location.origin}/${data.token}`;
            linkOutput.value = url;
            result.hidden = false;
            uploadArea.hidden = true;
        } catch (err) {
            alert(err.message);
            noteArea.hidden = false;
            uploadProgress.hidden = true;
            progressFill.style.width = '0%';
            sendBtn.disabled = false;
        }
    }
}

// ── PLAYER ──────────────────────────────────────────────

function initPlayer(token) {
    document.getElementById('view-player').hidden = false;

    const PAUSE_TIMEOUT = 15;
    const HEARTBEAT_INTERVAL = 5000;

    let sessionId = null;
    let audio = null;
    let heartbeatTimer = null;
    let countdownTimer = null;
    let countdownRemaining = PAUSE_TIMEOUT;

    const stateReady = document.getElementById('state-ready');
    const statePlaying = document.getElementById('state-playing');
    const statePaused = document.getElementById('state-paused');
    const stateGone = document.getElementById('state-gone');

    const playBtn = document.getElementById('play-btn');
    const pauseBtn = document.getElementById('pause-btn');
    const resumeBtn = document.getElementById('resume-btn');

    const progressFill = document.getElementById('progress-fill');
    const timeCurrent = document.getElementById('time-current');
    const timeTotal = document.getElementById('time-total');

    const pausedProgressFill = document.getElementById('paused-progress-fill');
    const pausedTimeCurrent = document.getElementById('paused-time-current');
    const pausedTimeTotal = document.getElementById('paused-time-total');

    const countdownArc = document.getElementById('countdown-arc');
    const countdownNumber = document.getElementById('countdown-number');
    const countdownRing = document.getElementById('countdown-ring');

    function formatTime(s) {
        const m = Math.floor(s / 60);
        const sec = Math.floor(s % 60);
        return `${m}:${sec.toString().padStart(2, '0')}`;
    }

    function showState(state) {
        stateReady.hidden = state !== 'ready';
        statePlaying.hidden = state !== 'playing';
        statePaused.hidden = state !== 'paused';
        stateGone.hidden = state !== 'gone';
    }

    function gone() {
        showState('gone');
        document.getElementById('note-display').hidden = true;
        if (audio) {
            audio.pause();
            audio.src = '';
        }
        clearInterval(heartbeatTimer);
        clearInterval(countdownTimer);
    }

    async function createSession() {
        const resp = await fetch(`${API}/session`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ token }),
        });
        if (!resp.ok) {
            gone();
            return null;
        }
        return resp.json();
    }

    async function sendHeartbeat() {
        if (!sessionId) return;
        try {
            const resp = await fetch(`${API}/heartbeat/${sessionId}`, { method: 'POST' });
            if (!resp.ok) gone();
        } catch {
            // Network error — don't kill immediately
        }
    }

    async function sendComplete() {
        if (!sessionId) return;
        try {
            await fetch(`${API}/complete/${sessionId}`, { method: 'POST' });
        } catch {
            // Best effort
        }
    }

    function updateProgress(currentTime, duration) {
        const pct = duration > 0 ? (currentTime / duration) * 100 : 0;
        progressFill.style.width = `${pct}%`;
        pausedProgressFill.style.width = `${pct}%`;
        timeCurrent.textContent = formatTime(currentTime);
        pausedTimeCurrent.textContent = formatTime(currentTime);
        timeTotal.textContent = formatTime(duration);
        pausedTimeTotal.textContent = formatTime(duration);
    }

    function startCountdown() {
        countdownRemaining = PAUSE_TIMEOUT;
        updateCountdownDisplay();

        countdownTimer = setInterval(() => {
            countdownRemaining -= 0.1;
            updateCountdownDisplay();

            if (countdownRemaining <= 0) {
                clearInterval(countdownTimer);
                sendComplete();
                gone();
            }
        }, 100);
    }

    function updateCountdownDisplay() {
        const pct = countdownRemaining / PAUSE_TIMEOUT;
        const offset = 283 * (1 - pct);
        countdownArc.style.strokeDashoffset = offset;
        countdownNumber.textContent = Math.ceil(countdownRemaining);

        countdownRing.classList.remove('warning', 'urgent');
        if (countdownRemaining <= 5) {
            countdownRing.classList.add('urgent');
        } else if (countdownRemaining <= 10) {
            countdownRing.classList.add('warning');
        }
    }

    playBtn.addEventListener('click', async () => {
        playBtn.disabled = true;
        playBtn.textContent = '...';

        const session = await createSession();
        if (!session) return;

        sessionId = session.session_id;

        if (session.note) {
            const noteDisplay = document.getElementById('note-display');
            noteDisplay.textContent = session.note;
            noteDisplay.hidden = false;
            document.getElementById('default-msg').hidden = true;
        }

        const streamResp = await fetch(`${API}/stream/${sessionId}`);
        if (!streamResp.ok) {
            gone();
            return;
        }
        const streamData = await streamResp.json();

        audio = new Audio(streamData.stream_url);

        audio.addEventListener('loadedmetadata', () => {
            updateProgress(0, audio.duration);
        });

        audio.addEventListener('timeupdate', () => {
            updateProgress(audio.currentTime, audio.duration);
        });

        audio.addEventListener('ended', () => {
            sendComplete();
            gone();
        });

        audio.addEventListener('error', () => {
            gone();
        });

        audio.play();
        showState('playing');

        heartbeatTimer = setInterval(sendHeartbeat, HEARTBEAT_INTERVAL);
    });

    pauseBtn.addEventListener('click', () => {
        if (!audio) return;
        audio.pause();
        showState('paused');
        startCountdown();
    });

    resumeBtn.addEventListener('click', () => {
        if (!audio) return;
        clearInterval(countdownTimer);
        audio.play();
        showState('playing');
    });

    showState('ready');
}
