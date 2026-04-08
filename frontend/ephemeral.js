const API = window.API_URL || '/api';

// Route: "/" = upload, anything else = player
const path = window.location.pathname.replace(/^\/+|\/+$/g, '');

function reveal(el) {
    el.hidden = false;
    el.classList.remove('fade-in');
    void el.offsetWidth; // reflow to restart animation
    el.classList.add('fade-in');
}

if (path === '') {
    initUpload();
} else if (path === 'how') {
    reveal(document.getElementById('view-how'));
} else {
    initPlayer(path);
}

// ── UPLOAD ──────────────────────────────────────────────

function initUpload() {
    reveal(document.getElementById('view-upload'));

    const uploadArea = document.getElementById('upload-area');
    const uploadTab = document.getElementById('upload-tab');
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
    let audioContext = null;
    let analyser = null;
    let levelRAF = null;

    // Tab switching
    tabUpload.addEventListener('click', () => {
        tabUpload.classList.add('active');
        tabRecord.classList.remove('active');
        uploadTab.hidden = false;
        recordArea.hidden = true;
    });

    tabRecord.addEventListener('click', () => {
        tabRecord.classList.add('active');
        tabUpload.classList.remove('active');
        uploadTab.hidden = true;
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
            cancelAnimationFrame(levelRAF);
            recordBtn.style.boxShadow = 'none';
            if (audioContext) audioContext.close();
            return;
        }

        try {
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            recordedChunks = [];
            recordSeconds = 0;

            // Use the MIME type the browser actually supports
            const recMime = MediaRecorder.isTypeSupported('audio/webm') ? 'audio/webm'
                          : MediaRecorder.isTypeSupported('audio/mp4')  ? 'audio/mp4'
                          : '';
            mediaRecorder = recMime
                ? new MediaRecorder(stream, { mimeType: recMime })
                : new MediaRecorder(stream);
            const actualMime = mediaRecorder.mimeType || recMime || 'audio/webm';
            const ext = actualMime.includes('mp4') ? 'mp4' : 'webm';

            mediaRecorder.ondataavailable = (e) => {
                if (e.data.size > 0) recordedChunks.push(e.data);
            };

            mediaRecorder.onstop = () => {
                stream.getTracks().forEach(t => t.stop());
                const blob = new Blob(recordedChunks, { type: actualMime });

                if (blob.size > 5 * 1024 * 1024) {
                    recordStatus.textContent = 'too long (max 5 MB)';
                    recordTime.hidden = true;
                    return;
                }

                selectedFile = new File([blob], `recording.${ext}`, { type: actualMime });
                computeWaveform(selectedFile).then(w => { waveformData = w; });
                recordArea.hidden = true;
                uploadTab.hidden = true;
                document.querySelector('.input-toggle').hidden = true;
                reveal(noteArea);
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

            // Volume indicator
            audioContext = new AudioContext();
            if (audioContext.state === 'suspended') await audioContext.resume();
            const source = audioContext.createMediaStreamSource(stream);
            analyser = audioContext.createAnalyser();
            analyser.fftSize = 256;
            source.connect(analyser);
            const dataArray = new Uint8Array(analyser.frequencyBinCount);

            function updateLevel() {
                analyser.getByteFrequencyData(dataArray);
                let sum = 0;
                for (let i = 0; i < dataArray.length; i++) sum += dataArray[i];
                const avg = sum / dataArray.length;
                const level = Math.min(avg / 80, 1); // normalize
                const glow = Math.round(level * 20);
                recordBtn.style.boxShadow = level > 0.1
                    ? `0 0 ${glow}px ${Math.round(glow/2)}px rgba(204, 51, 51, ${level * 0.6})`
                    : 'none';
                levelRAF = requestAnimationFrame(updateLevel);
            }
            updateLevel();
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
        slugInput.value = slugInput.value
            .toLowerCase()
            .replace(/\s+/g, '-')         // spaces → hyphens
            .replace(/[^a-z0-9\-]/g, '')  // strip anything else
            .replace(/-{2,}/g, '-');      // collapse runs of hyphens
    });

    sendBtn.addEventListener('click', () => upload());

    const copyIconHTML = copyBtn.innerHTML;
    const checkIconHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;

    copyBtn.addEventListener('click', () => {
        linkOutput.select();
        navigator.clipboard.writeText(linkOutput.value);
        copyBtn.innerHTML = checkIconHTML;
        setTimeout(() => { copyBtn.innerHTML = copyIconHTML; }, 2000);
    });

    // Native share sheet on mobile
    const shareBtn = document.getElementById('share-btn');
    if (navigator.share) {
        shareBtn.hidden = false;
        shareBtn.addEventListener('click', () => {
            navigator.share({
                title: 'ephemeral',
                text: 'Listen once, then it\'s gone.',
                url: linkOutput.value,
            });
        });
    }

    let waveformData = '';

    async function computeWaveform(file) {
        try {
            const arrayBuffer = await file.arrayBuffer();
            const audioCtx = new AudioContext();
            if (audioCtx.state === 'suspended') await audioCtx.resume();
            const audioBuffer = await audioCtx.decodeAudioData(arrayBuffer);
            const raw = audioBuffer.getChannelData(0);
            const samples = 100;
            const blockSize = Math.floor(raw.length / samples);
            const peaks = [];
            for (let i = 0; i < samples; i++) {
                let sum = 0;
                for (let j = 0; j < blockSize; j++) {
                    sum += Math.abs(raw[i * blockSize + j]);
                }
                peaks.push(sum / blockSize);
            }
            const max = Math.max(...peaks);
            audioCtx.close();
            return peaks.map(p => Math.round((p / max) * 100)).join(',');
        } catch {
            return '';
        }
    }

    function selectFile(file) {
        const audioExt = /\.(mp3|m4a|wav|aac|ogg|flac|opus|webm|mp4|aiff?)$/i;
        const isAudio = file.type.startsWith('audio/') || audioExt.test(file.name);
        if (!isAudio) {
            alert('Please select an audio file.');
            return;
        }
        if (file.size > 5 * 1024 * 1024) {
            alert('File too large (max 5 MB).');
            return;
        }
        selectedFile = file;
        computeWaveform(file).then(w => { waveformData = w; });
        uploadTab.hidden = true;
        recordArea.hidden = true;
        document.querySelector('.input-toggle').hidden = true;
        reveal(noteArea);
        slugInput.focus();
    }

    async function upload() {
        if (!selectedFile) return;

        slugError.hidden = true;
        noteArea.hidden = true;
        uploadProgress.hidden = false;
        sendBtn.disabled = true;
        progressFill.style.width = '0%';

        try {
            const resp = await fetch(`${API}/upload`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    slug: slugInput.value.trim() || undefined,
                    note: noteInput.value.trim() || undefined,
                    waveform: waveformData || undefined,
                    content_type: selectedFile.type,
                }),
            });

            if (resp.status === 409) {
                slugError.hidden = false;
                slugInput.focus();
                slugInput.select();
                reveal(noteArea);
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

            // Show link immediately — optimistic
            const url = `${window.location.origin}/${data.token}`;
            linkOutput.value = url;
            uploadProgress.hidden = true;
            document.querySelector('.tab-content').hidden = true;
            document.querySelector('.input-toggle').hidden = true;
            reveal(result);
            uploadArea.hidden = true;
            document.querySelector('.input-toggle').hidden = true;

            // Upload to S3 in background
            const xhr = new XMLHttpRequest();
            xhr.open('PUT', data.upload_url);
            xhr.setRequestHeader('Content-Type', selectedFile.type);
            xhr.send(selectedFile);
        } catch (err) {
            alert(err.message);
            reveal(noteArea);
            uploadProgress.hidden = true;
            progressFill.style.width = '0%';
            sendBtn.disabled = false;
        }
    }
}

// ── PLAYER ──────────────────────────────────────────────

function initPlayer(token) {
    reveal(document.getElementById('view-player'));

    const PAUSE_TIMEOUT = 15;
    const HEARTBEAT_INTERVAL = 5000;

    let sessionId = null;
    let audio = null;
    let heartbeatTimer = null;
    let countdownTimer = null;
    let playRAF = null;
    let countdownRemaining = PAUSE_TIMEOUT;

    const stateReady = document.getElementById('state-ready');
    const statePlaying = document.getElementById('state-playing');
    const statePaused = document.getElementById('state-paused');
    const stateGone = document.getElementById('state-gone');

    const playBtn = document.getElementById('play-btn');
    const pillFill = document.getElementById('pill-fill');
    const pillLabel = document.getElementById('pill-label');
    const pauseBtn = document.getElementById('pause-btn');
    const playFill = document.getElementById('play-fill');
    const resumeBtn = document.getElementById('resume-btn');

    const timeCurrent = document.getElementById('time-current');
    const timeTotal = document.getElementById('time-total');

    const countdownArc = document.getElementById('countdown-arc');
    const countdownNumber = document.getElementById('countdown-number');
    const countdownRing = document.getElementById('resume-btn');

    function makeWaveformPath(points) {
        let path = `M0,${100 - points[0] * 0.4 - 30}`;
        for (let i = 1; i < points.length; i++) {
            path += ` L${i},${100 - points[i] * 0.4 - 30}`;
        }
        path += ` L${points.length - 1},100 L0,100 Z`;
        return path;
    }

    function renderWaveform(btn, fillEl, points) {
        const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
        svg.setAttribute('viewBox', `0 0 ${points.length} 100`);
        svg.setAttribute('preserveAspectRatio', 'none');
        svg.classList.add('waveform-svg');
        const pathEl = document.createElementNS('http://www.w3.org/2000/svg', 'path');
        pathEl.setAttribute('d', makeWaveformPath(points));
        pathEl.setAttribute('fill', 'rgba(255,255,255,0.3)');
        svg.appendChild(pathEl);
        fillEl.appendChild(svg);
    }

    function formatTime(s) {
        const m = Math.floor(s / 60);
        const sec = Math.floor(s % 60);
        return `${m}:${sec.toString().padStart(2, '0')}`;
    }

    function showState(state) {
        [stateReady, statePlaying, statePaused, stateGone].forEach(el => el.hidden = true);
        const active = { ready: stateReady, playing: statePlaying, paused: statePaused, gone: stateGone }[state];
        if (active) reveal(active);
    }

    function gone(played) {
        if (played) {
            document.getElementById('gone-msg').textContent = 'now it\'s in your head.';
            if (navigator.vibrate) navigator.vibrate(200);
        }
        showState('gone');
        document.body.classList.remove('breathing', 'breath-held');
        document.getElementById('note-display').hidden = true;
        cancelAnimationFrame(playRAF);
        if (audio) {
            audio.pause();
            audio.removeAttribute('src');
            audio.load();
            audio = null;
        }
        clearInterval(heartbeatTimer);
        clearInterval(countdownTimer);
    }

    async function createSession() {
        try {
            const resp = await fetch(`${API}/session`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ token }),
            });
            if (!resp.ok) return null;
            return resp.json();
        } catch {
            return null;
        }
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
        playFill.style.width = `${pct}%`;
        timeCurrent.textContent = formatTime(currentTime);
        timeTotal.textContent = formatTime(duration);
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
                gone(true);
            }
        }, 100);
    }

    function updateCountdownDisplay() {
        const pct = countdownRemaining / PAUSE_TIMEOUT;
        const offset = 283 * (1 - pct);
        countdownArc.style.strokeDashoffset = offset;

        const countdownPlay = document.querySelector('.countdown-play');

        countdownRing.classList.remove('warning', 'urgent');
        if (countdownRemaining <= 5) {
            countdownRing.classList.add('urgent');
            countdownNumber.hidden = false;
            countdownNumber.textContent = Math.ceil(countdownRemaining);
            if (countdownPlay) countdownPlay.hidden = true;
        } else if (countdownRemaining <= 10) {
            countdownRing.classList.add('warning');
        }
    }

    // Check if token exists before showing play UI
    let tokenNote = null;
    let tokenWaveform = null;
    let preloadedStreamUrl = null;

    (async () => {
        try {
            const resp = await fetch(`${API}/check/${token}`);
            const data = await resp.json();
            if (data.exists === 'true') {
                tokenNote = data.note || null;
                tokenWaveform = data.waveform ? data.waveform.split(',').map(Number) : null;
                preloadedStreamUrl = data.stream_url || null;
                showState('ready');
            } else {
                gone();
            }
        } catch {
            gone();
        }
    })();

    let startTimer = null;
    let startRAF = null;
    let cancelled = false;

    let counting = false;

    playBtn.addEventListener('click', () => {
        if (counting) {
            // Cancel
            cancelled = true;
            counting = false;
            cancelAnimationFrame(startRAF);
            pillLabel.textContent = '▶ play once';
            pillLabel.style.opacity = 1;
            pillFill.style.width = '0%';
            document.getElementById('note-display').hidden = true;
            document.getElementById('default-msg').hidden = false;
            // Clean up primed audio
            if (audio) {
                audio.pause();
                audio.removeAttribute('src');
                audio.load();
                audio = null;
            }
            return;
        }

        cancelled = false;
        counting = true;

        // Prime audio in the user-gesture context so iOS allows playback
        if (preloadedStreamUrl) {
            audio = new Audio(preloadedStreamUrl);
            audio.preload = 'auto';
            audio.volume = 0;
            const unlock = audio.play();
            if (unlock) unlock.then(() => {
                audio.pause();
                audio.currentTime = 0;
                audio.volume = 1;
            }).catch(() => {});
        }

        pillLabel.textContent = 'save for later';
        pillFill.style.transition = 'none';
        pillFill.style.width = '0%';

        // Show note during countdown
        if (tokenNote) {
            const noteDisplay = document.getElementById('note-display');
            noteDisplay.textContent = tokenNote;
            reveal(noteDisplay);
            document.getElementById('default-msg').hidden = true;
        }

        const startTime = Date.now();
        const duration = 3000;

        function tick() {
            if (cancelled) return;
            const elapsed = Date.now() - startTime;
            const pct = Math.min(elapsed / duration, 1);
            pillFill.style.width = `${pct * 100}%`;
            pillLabel.style.opacity = 1 - pct;

            if (pct < 1) {
                startRAF = requestAnimationFrame(tick);
            } else {
                counting = false;
                startPlaying();
            }
        }
        startRAF = requestAnimationFrame(tick);
    });

    async function startPlaying() {
        // audio was primed in the click handler; fall back to creating it if needed
        if (!audio && preloadedStreamUrl) {
            audio = new Audio(preloadedStreamUrl);
        }
        if (!audio) { gone(); return; }

        // Smooth progress via rAF instead of timeupdate
        function tickPlayback() {
            if (!audio) return;
            updateProgress(audio.currentTime, audio.duration);

            // Burn token after 1 second of confirmed playback
            if (audio.currentTime >= 1 && !sessionId) {
                sessionId = 'pending';
                (async () => {
                    const session = await createSession();
                    if (session) {
                        sessionId = session.session_id;
                        heartbeatTimer = setInterval(sendHeartbeat, HEARTBEAT_INTERVAL);
                    }
                })();
            }

            playRAF = requestAnimationFrame(tickPlayback);
        }
        playRAF = requestAnimationFrame(tickPlayback);

        // Stop rAF loop when audio ends or errors
        audio.addEventListener('ended', () => { cancelAnimationFrame(playRAF); });
        audio.addEventListener('error', () => { cancelAnimationFrame(playRAF); });

        audio.addEventListener('ended', () => {
            sendComplete();
            gone(true);
        });

        audio.addEventListener('error', () => {
            if (!audio) return;
            gone();
        });

        if (tokenWaveform) renderWaveform(pauseBtn, playFill, tokenWaveform);
        audio.play();
        showState('playing');
        document.body.classList.add('breathing');
        document.body.classList.remove('breath-held');
    }

    pauseBtn.addEventListener('click', () => {
        if (!audio) return;
        audio.pause();
        showState('paused');
        document.body.classList.add('breath-held');
        startCountdown();
    });

    resumeBtn.addEventListener('click', () => {
        if (!audio) return;
        clearInterval(countdownTimer);
        audio.play();
        showState('playing');
        document.body.classList.remove('breath-held');
    });

}
