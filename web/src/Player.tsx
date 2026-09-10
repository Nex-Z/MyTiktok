import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  Bookmark,
  Heart,
  Maximize2,
  Music2,
  RotateCcw,
  EyeOff,
  Film,
  Play,
  Volume2,
  VolumeX,
  Info,
  X,
} from "lucide-react";
import { time, type Video } from "./api";
import useMediaZoom from "./useMediaZoom";
import VideoDetails from "./VideoDetails";
export default function Player({
  video: v,
  nextVideo,
  muted,
  setMuted,
  resume,
  onMove,
  onFavorite,
  onLike,
  onHide,
  notify,
  wheelGesture,
  variant = "immersive",
}: {
  video: Video;
  nextVideo?: Video;
  muted: boolean;
  setMuted: (v: boolean) => void;
  resume: boolean;
  onMove: (d: number) => void;
  onFavorite: () => void;
  onLike: () => void;
  onHide: () => void;
  notify: (s: string) => void;
  wheelGesture: { current: number };
  variant?: "immersive" | "preview";
}) {
  const zoom = useMediaZoom();
  const isImage = v.mediaType === "image",
    isAudio = v.mediaType === "audio";
  const MediaTag = isAudio ? "audio" : "video";
  useEffect(() => {
    if (isAudio) setMuted(false);
  }, [isAudio, setMuted]);
  const ref = useRef<HTMLVideoElement>(null),
    lastElement = useRef<HTMLVideoElement | null>(null),
    lastSave = useRef(0),
    played = useRef(false),
    touch = useRef<number | null>(null),
    lastMove = useRef(0),
    attemptPosition = useRef(resume ? v?.position : 0),
    moveRef = useRef(onMove);
  moveRef.current = onMove;
  const [stage, setStage] = useState(0),
    [blocked, setBlocked] = useState(false),
    [failure, setFailure] = useState(""),
    [isPlaying, setPlaying] = useState(false),
    [position, setPosition] = useState(0),
    [duration, setDuration] = useState(v?.duration || 0);
  useEffect(() => {
    if (!isPlaying || !nextVideo || nextVideo.kind !== "p115") return;
    void fetch(`/api/videos/${nextVideo.id}/prepare`).catch(() => {});
  }, [isPlaying, nextVideo?.id, nextVideo?.kind]);
  const shell = useRef<HTMLDivElement>(null);
  const [compact, setCompact] = useState(false),
    [wide, setWide] = useState(false);
  const [controlsVisible, setControlsVisible] = useState(true),
    [interacting, setInteracting] = useState(false),
    [activity, setActivity] = useState(0);
  const reveal = () => {
    setControlsVisible(true);
    setActivity((n) => n + 1);
  };
  useEffect(() => {
    const media = matchMedia("(pointer: coarse), (max-width: 700px)");
    const update = () => {
      setCompact(media.matches);
      setWide(media.matches && innerWidth > innerHeight);
      setControlsVisible(true);
      setActivity((n) => n + 1);
    };
    update();
    media.addEventListener("change", update);
    window.addEventListener("resize", update);
    return () => {
      media.removeEventListener("change", update);
      window.removeEventListener("resize", update);
    };
  }, []);
  useEffect(() => {
    const release = () => setInteracting(false);
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", release);
    return () => {
      window.removeEventListener("pointerup", release);
      window.removeEventListener("pointercancel", release);
    };
  }, []);
  const fullscreen = async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else if (shell.current?.requestFullscreen)
        await shell.current.requestFullscreen();
      else {
        const el = ref.current as
          | (HTMLVideoElement & { webkitEnterFullscreen?: () => void })
          | null;
        if (el?.webkitEnterFullscreen) el.webkitEnterFullscreen();
        else notify("当前浏览器不支持全屏，请横屏观看");
      }
    } catch {
      notify("未能进入全屏，请重试");
    }
  };
  const [showInfo, setShowInfo] = useState(false);
  useEffect(() => {
    if (
      !compact ||
      !isPlaying ||
      showInfo ||
      interacting ||
      blocked ||
      failure
    ) {
      setControlsVisible(true);
      return;
    }
    const timer = setTimeout(() => {
      if (
        !shell.current?.querySelector(
          "input:focus-visible,button:focus-visible",
        )
      )
        setControlsVisible(false);
    }, 2400);
    return () => clearTimeout(timer);
  }, [compact, isPlaying, showInfo, interacting, blocked, failure, activity]);
  const infoDialog = useRef<HTMLDialogElement>(null),
    pressTimer = useRef<ReturnType<typeof setTimeout> | null>(null),
    pressX = useRef(0),
    longPressed = useRef(false),
    suppressClickUntil = useRef(0),
    resumeAfterInfo = useRef(false);
  const cancelPress = () => {
    if (pressTimer.current) clearTimeout(pressTimer.current);
    pressTimer.current = null;
  };
  useEffect(
    () => () => {
      if (pressTimer.current) clearTimeout(pressTimer.current);
    },
    [],
  );
  useEffect(() => {
    if (showInfo) infoDialog.current?.showModal();
    else infoDialog.current?.close();
  }, [showInfo]);
  const save = useCallback(() => {
    const el = ref.current || lastElement.current;
    if (!el || !played.current) return;
    void fetch(`/api/videos/${v.id}/history`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ position: el.currentTime }),
      keepalive: true,
    }).catch(() => {});
  }, [v.id]);
  const openInfo = () => {
    cancelPress();
    save();
    resumeAfterInfo.current = !!ref.current && !ref.current.paused;
    ref.current?.pause();
    setShowInfo(true);
  };
  const closeInfo = () => {
    setShowInfo(false);
    if (resumeAfterInfo.current && !document.hidden)
      void ref.current?.play().catch(() => setBlocked(true));
  };
  const move = useCallback(
    (d: number) => {
      if (Date.now() - lastMove.current < 500) return;
      lastMove.current = Date.now();
      save();
      moveRef.current(d);
    },
    [save],
  );
  useEffect(() => {
    const el = ref.current;
    lastElement.current = el;
    const key = (e: KeyboardEvent) => {
      if (document.querySelector("dialog[open]")) return;
      if (e.repeat) return;
      if ((e.target as HTMLElement).closest("input,select,textarea,button"))
        return;
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        move(e.key === "ArrowDown" ? 1 : -1);
      }
    };
    const visibility = () => {
      if (document.hidden) {
        save();
        el?.pause();
      }
    };
    window.addEventListener("keydown", key);
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pagehide", save);
    return () => {
      save();
      el?.pause();
      window.removeEventListener("keydown", key);
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("pagehide", save);
    };
  }, [save, move]);
  if (!v) return null;
  const toggle = () => {
    if (Date.now() < suppressClickUntil.current) return;
    const el = ref.current;
    if (!el) return;
    if (el.paused)
      void el
        .play()
        .then(() => setBlocked(false))
        .catch(() => setBlocked(true));
    else el.pause();
  };
  const tapVideo = () => {
    if (Date.now() < suppressClickUntil.current) return;
    if (wide && !controlsVisible && isPlaying) {
      reveal();
      return;
    }
    reveal();
    toggle();
  };
  const fail = () => {
    const el = ref.current;
    if (el) attemptPosition.current = el.currentTime || attemptPosition.current;
    if (el?.error?.code === 3) {
      setFailure("视频编码不兼容或文件损坏，请切换下一条");
      return;
    }
    if (v.kind === "p115" && stage < 1) {
      setStage(1);
      return;
    }
    setFailure("暂时无法播放。请检查媒体源连接或视频编码。");
  };
  const src = `/api/videos/${v.id}/play?${stage === 1 ? "proxy=1" : ""}`;
  return (
    <div
      ref={shell}
      className={`player-layout ${isImage ? "image-player" : isAudio ? "audio-player" : ""} ${variant} ${v.width > v.height ? "landscape" : "portrait"} ${compact ? "touch-player" : ""} ${wide ? "touch-wide" : ""} ${controlsVisible ? "controls-visible" : "controls-hidden"}`}
    >
      <section
        className="stage"
        onPointerDownCapture={(e) => {
          if ((e.target as HTMLElement).closest("button,input")) {
            reveal();
            setInteracting(true);
          }
        }}
        onPointerUpCapture={() => {
          setInteracting(false);
        }}
        onPointerCancel={() => setInteracting(false)}
      >
        <div
          className="video-shell"
          onWheel={(e) => {
            if (zoom.view.scale > 1) return;
            const now = Date.now();
            const fresh = now - wheelGesture.current > 180;
            wheelGesture.current = now;
            if (fresh && Math.abs(e.deltaY) > 20) move(e.deltaY > 0 ? 1 : -1);
          }}
          onTouchStart={(e) => {
            cancelPress();
            longPressed.current = false;
            if (!isAudio && zoom.start(e)) {
              touch.current = null;
              suppressClickUntil.current = Date.now() + 800;
              return;
            }
            if (
              e.touches.length !== 1 ||
              (e.target as HTMLElement).closest("button,input")
            )
              return;
            touch.current = e.touches[0].clientY;
            pressX.current = e.touches[0].clientX;
            pressTimer.current = setTimeout(() => {
              longPressed.current = true;
              suppressClickUntil.current = Date.now() + 800;
              openInfo();
            }, 550);
          }}
          onTouchMove={(e) => {
            if (!isAudio && zoom.move(e)) {
              cancelPress();
              touch.current = null;
              suppressClickUntil.current = Date.now() + 800;
              return;
            }
            if (
              touch.current !== null &&
              (Math.abs(e.touches[0].clientY - touch.current) > 12 ||
                Math.abs(e.touches[0].clientX - pressX.current) > 12)
            )
              cancelPress();
          }}
          onTouchEnd={(e) => {
            cancelPress();
            if (!isAudio && zoom.end(e)) {
              touch.current = null;
              suppressClickUntil.current = Date.now() + 800;
              return;
            }
            if (longPressed.current) {
              touch.current = null;
              suppressClickUntil.current = Date.now() + 800;
              return;
            }
            if (touch.current === null) return;
            const d = touch.current - e.changedTouches[0].clientY;
            touch.current = null;
            if (Math.abs(d) > 65) {
              suppressClickUntil.current = Date.now() + 800;
              move(d > 0 ? 1 : -1);
            }
          }}
          onTouchCancel={() => {
            zoom.reset();
            cancelPress();
            touch.current = null;
          }}
          onContextMenu={(e) => e.preventDefault()}
        >
          {isImage ? (
            <img
              className="media-picture"
              src={src}
              alt={v.title}
              draggable={false}
              style={zoom.style}
              onClick={reveal}
              onLoad={() => {
                void fetch("/api/videos/" + v.id + "/history", {
                  method: "PUT",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ position: 0 }),
                }).catch(() => {});
              }}
              onError={fail}
            />
          ) : (
            <MediaTag
              className={isAudio ? "audio-element" : "media-video"}
              style={isAudio ? undefined : zoom.style}
              ref={(el) => {
                ref.current = el as HTMLVideoElement | null;
              }}
              src={src}
              poster={v.cover || undefined}
              muted={muted}
              playsInline
              loop
              preload="auto"
              autoPlay
              onLoadedMetadata={() => {
                const el = ref.current;
                if (!el) return;
                if (Number.isFinite(el.duration)) setDuration(el.duration);
                if (
                  attemptPosition.current > 0 &&
                  attemptPosition.current < el.duration
                )
                  el.currentTime = attemptPosition.current;
                void el.play().catch(() => setBlocked(true));
              }}
              onPlaying={() => {
                played.current = true;
                setPlaying(true);
                setBlocked(false);
                setFailure("");
                if (!lastSave.current) {
                  save();
                  lastSave.current = Date.now();
                }
              }}
              onPause={() => setPlaying(false)}
              onTimeUpdate={() => {
                const el = ref.current;
                if (!el) return;
                setPosition(el.currentTime);
                if (played.current && Date.now() - lastSave.current > 5000) {
                  save();
                  lastSave.current = Date.now();
                }
              }}
              onError={fail}
              onClick={tapVideo}
            />
          )}
          {isAudio && (
            <div className="audio-canvas" onClick={tapVideo}>
              <Music2 size={68} />
              <h2>{v.title}</h2>
              <span>{isPlaying ? "正在播放音频" : "音频"}</span>
            </div>
          )}
          {!isAudio && zoom.view.scale > 1.01 && (
            <button
              className="zoom-reset"
              aria-label="恢复缩放"
              onClick={() => {
                zoom.reset();
                suppressClickUntil.current = Date.now() + 500;
              }}
            >
              <RotateCcw size={17} />
              恢复缩放
            </button>
          )}
          {!isImage && !isPlaying && played.current && !blocked && !failure && (
            <button
              className="paused-center"
              aria-label="继续播放"
              onClick={toggle}
            >
              <Play size={64} fill="currentColor" />
            </button>
          )}
          {(blocked || failure) && (
            <div className="play-message">
              {failure ? (
                <>
                  <Film />
                  <p>{failure}</p>
                  <button className="primary" onClick={() => move(1)}>
                    下一条
                  </button>
                </>
              ) : (
                <button
                  className="large-play"
                  onClick={toggle}
                  aria-label="点击播放"
                >
                  <Play size={32} />
                </button>
              )}
            </div>
          )}
          <div className="video-bottom">
            <h2>{v.title}</h2>
            {!isImage && (
              <div className="transport">
                <span>{time(position)}</span>
                <input
                  aria-label="播放进度"
                  type="range"
                  min="0"
                  max={duration || 1}
                  step="0.1"
                  value={Math.min(position, duration || 1)}
                  onChange={(e) => {
                    reveal();
                    const p = Number(e.target.value);
                    if (ref.current) ref.current.currentTime = p;
                    setPosition(p);
                  }}
                />
                <span>{time(duration)}</span>
              </div>
            )}
          </div>
        </div>
        <aside className="player-actions">
          {!isImage && muted && (
            <button
              className="action mute-action"
              aria-label="开启声音"
              onClick={() => setMuted(false)}
            >
              <VolumeX />
            </button>
          )}
          <button
            className={v.liked ? "action selected" : "action"}
            aria-label={v.liked ? "取消喜欢" : "喜欢"}
            onClick={onLike}
          >
            <Heart fill={v.liked ? "currentColor" : "none"} />
          </button>

          <button
            className={v.favorite ? "action selected" : "action"}
            onClick={onFavorite}
            aria-label={v.favorite ? "取消收藏" : "收藏"}
          >
            <Bookmark fill={v.favorite ? "currentColor" : "none"} />
          </button>
          <button
            className="action fullscreen-action"
            aria-label="切换全屏"
            onClick={() => void fullscreen()}
          >
            <Maximize2 />
          </button>
          <button
            className="action info-action"
            onClick={openInfo}
            aria-label="视频信息"
          >
            <Info />
          </button>
          <div className="action-spacer" />
          <button
            className="round"
            onClick={() => move(-1)}
            aria-label="上一条"
          >
            <ArrowUp />
          </button>
          <button className="round" onClick={() => move(1)} aria-label="下一条">
            <ArrowDown />
          </button>
        </aside>
        {nextVideo &&
          nextVideo.kind !== "p115" &&
          nextVideo.mediaType !== "image" && (
          <video
            className="preload"
            muted
            preload="metadata"
            src={`/api/videos/${nextVideo.id}/play`}
          />
        )}
      </section>
      <div className="desktop-video-details">
        <VideoDetails video={v} duration={duration} />
      </div>
      <dialog
        className="info-sheet"
        ref={infoDialog}
        aria-label="视频基础信息"
        onCancel={(e) => {
          e.preventDefault();
          closeInfo();
        }}
        onClick={(e) => {
          if (e.target === e.currentTarget) closeInfo();
        }}
      >
        <div className="sheet-handle" />
        <header>
          <h2>视频信息</h2>
          <button
            className="icon-button"
            aria-label="关闭视频信息"
            onClick={closeInfo}
          >
            <X size={20} />
          </button>
        </header>
        <VideoDetails video={v} duration={duration} />
        <div className="sheet-tools">
          <button className="secondary" onClick={() => setMuted(!muted)}>
            {muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
            {muted ? "开启声音" : "静音"}
          </button>
          <button
            className="secondary"
            onClick={() => {
              closeInfo();
              onHide();
            }}
          >
            <EyeOff size={18} />
            隐藏视频
          </button>
        </div>
      </dialog>
    </div>
  );
}
