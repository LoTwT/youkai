; Compile-time sentinel. A direct upstream build must never publish its
; unadapted force-killing installer. Runtime checks are in both CloseApp functions.
!ifndef YOUKAI_SAFE_INSTALLER
  !error "Use go run ./tools/mygobuild for Windows packaging (MyGo 0.9 safety adapter required)"
!endif
