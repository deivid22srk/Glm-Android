import { useState, useCallback } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { AnimatedGridPattern } from './animated-grid-pattern'
import { cn } from '@/lib/utils'

const slides = [
  {
    title: 'Bem-vindo ao glm5.2proxy',
    description: 'Seu proxy inteligente para gerenciar múltiplas contas ZCode com rotação automática e controle total.',
    icon: (
      <svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round" className="text-primary">
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10" />
        <path d="m9 12 2 2 4-4" />
      </svg>
    ),
  },
  {
    title: 'Gestão Inteligente de Contas',
    description: 'Adicione múltiplas contas, monitore cotas em tempo real e deixe o sistema alternar automaticamente quando necessário.',
    icon: (
      <svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round" className="text-primary">
        <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
        <circle cx="9" cy="7" r="4" />
        <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
        <path d="M16 3.13a4 4 0 0 1 0 7.75" />
      </svg>
    ),
  },
  {
    title: 'Compatível com OpenAI',
    description: 'API drop-in compatível com clientes OpenAI. Basta apontar para a porta do proxy e usar sua chave.',
    icon: (
      <svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round" className="text-primary">
        <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
        <path d="M5 3v4" /><path d="M19 17v4" /><path d="M3 5h4" /><path d="M17 19h4" />
      </svg>
    ),
  },
]

const STORAGE_KEY = 'hasSeenOnboarding'

const slideVariants = {
  enter: (direction: number) => ({
    x: direction > 0 ? 80 : -80,
    opacity: 0,
    scale: 0.96,
  }),
  center: {
    x: 0,
    opacity: 1,
    scale: 1,
  },
  exit: (direction: number) => ({
    x: direction < 0 ? 80 : -80,
    opacity: 0,
    scale: 0.96,
  }),
}

interface OnboardingProps {
  onComplete: () => void
}

export function Onboarding({ onComplete }: OnboardingProps) {
  const [[page, direction], setPage] = useState([0, 0])
  const current = slides[page]
  const isLast = page === slides.length - 1

  const paginate = useCallback(
    (newDirection: number) => {
      const next = page + newDirection
      if (next < 0 || next >= slides.length) return
      setPage([next, newDirection])
    },
    [page]
  )

  const handleFinish = () => {
    try {
      localStorage.setItem(STORAGE_KEY, 'true')
    } catch {}
    onComplete()
  }

  return (
    <div className="relative flex h-screen w-full items-center justify-center overflow-hidden bg-background">
      <AnimatedGridPattern
        numSquares={40}
        maxOpacity={0.15}
        duration={5}
        repeatDelay={1}
        className="opacity-40"
      />

      <div className="absolute inset-0 bg-gradient-to-br from-background via-transparent to-background/80 pointer-events-none" />

      <div className="relative z-10 flex w-full max-w-lg flex-col items-center px-6">
        <div className="relative w-full min-h-[380px] flex items-center justify-center">
          <AnimatePresence initial={false} custom={direction} mode="wait">
            <motion.div
              key={page}
              custom={direction}
              variants={slideVariants}
              initial="enter"
              animate="center"
              exit="exit"
              transition={{ type: 'spring', stiffness: 300, damping: 30 }}
              className="absolute w-full"
            >
              <div className="flex flex-col items-center text-center rounded-2xl border border-border/50 bg-card/60 backdrop-blur-xl p-10 shadow-2xl">
                <motion.div
                  initial={{ scale: 0.8, opacity: 0 }}
                  animate={{ scale: 1, opacity: 1 }}
                  transition={{ delay: 0.1, type: 'spring', stiffness: 200 }}
                  className="mb-6 flex h-20 w-20 items-center justify-center rounded-2xl bg-primary/10 ring-1 ring-primary/20"
                >
                  {current.icon}
                </motion.div>

                <motion.h1
                  initial={{ y: 10, opacity: 0 }}
                  animate={{ y: 0, opacity: 1 }}
                  transition={{ delay: 0.15 }}
                  className="text-2xl font-semibold tracking-tight text-foreground"
                >
                  {current.title}
                </motion.h1>

                <motion.p
                  initial={{ y: 10, opacity: 0 }}
                  animate={{ y: 0, opacity: 1 }}
                  transition={{ delay: 0.2 }}
                  className="mt-3 text-sm leading-relaxed text-muted-foreground max-w-sm"
                >
                  {current.description}
                </motion.p>
              </div>
            </motion.div>
          </AnimatePresence>
        </div>

        <div className="mt-8 flex items-center gap-6">
          <button
            type="button"
            onClick={() => paginate(-1)}
            disabled={page === 0}
            className={cn(
              'flex h-10 w-10 items-center justify-center rounded-xl border border-border/60 bg-card/40 backdrop-blur-sm',
              'text-muted-foreground transition-all duration-200',
              'hover:bg-accent hover:text-foreground hover:border-border',
              'active:scale-95',
              'disabled:opacity-30 disabled:cursor-not-allowed disabled:hover:bg-card/40'
            )}
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m15 18-6-6 6-6" /></svg>
          </button>

          <div className="flex gap-2">
            {slides.map((_, i) => (
              <div
                key={i}
                className={cn(
                  'h-1.5 rounded-full transition-all duration-300',
                  i === page ? 'w-6 bg-foreground' : 'w-1.5 bg-muted-foreground/30'
                )}
              />
            ))}
          </div>

          {isLast ? (
            <button
              type="button"
              onClick={handleFinish}
              className={cn(
                'flex h-10 items-center gap-2 rounded-xl px-5',
                'bg-foreground text-background font-medium text-sm',
                'transition-all duration-200 hover:opacity-90 active:scale-95',
                'shadow-lg shadow-foreground/10'
              )}
            >
              Começar
              <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M5 12h14" /><path d="m12 5 7 7-7 7" /></svg>
            </button>
          ) : (
            <button
              type="button"
              onClick={() => paginate(1)}
              className={cn(
                'flex h-10 w-10 items-center justify-center rounded-xl border border-border/60 bg-card/40 backdrop-blur-sm',
                'text-muted-foreground transition-all duration-200',
                'hover:bg-accent hover:text-foreground hover:border-border',
                'active:scale-95'
              )}
            >
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m9 18 6-6-6-6" /></svg>
            </button>
          )}
        </div>

        <button
          type="button"
          onClick={handleFinish}
          className="mt-6 text-xs text-muted-foreground/50 transition-colors hover:text-muted-foreground"
        >
          Pular introdução
        </button>
      </div>
    </div>
  )
}
