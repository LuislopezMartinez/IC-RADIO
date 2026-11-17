/* IC-RADIO audio-only adapter for WDSP 2.11.
 *
 * WDSP's public OpenChannel path expects complex baseband I/Q and performs
 * demodulation. IC-RADIO receives already-demodulated PCM from a radio USB
 * codec, so this adapter invokes the mature RX noise stages directly.
 */
#include "comm.h"

#define IC_BLOCK_SIZE 256

static double *ic_buffer;
static NNR ic_nnr;
static EMNR ic_nr2;
static ANF ic_anf;
static SNBA ic_blanker;
static int ic_model;
static int ic_nnr_strength;

static double ic_effective_strength(int reduction, int protection) {
    double strength = (double)reduction / 100.0;
    strength *= 1.0 - 0.5 * (double)protection / 100.0;
    if (strength < 0.0) strength = 0.0;
    if (strength > 1.0) strength = 1.0;
    return strength;
}

static int ic_recreate_nnr(int strength, int model) {
    double floor_db = -3.0 - 37.0 * (double)strength / 100.0;
    if (ic_nnr) destroy_nnr(ic_nnr);
    ic_nnr = create_nnr(1, 0, IC_BLOCK_SIZE, ic_buffer, ic_buffer,
                        48000, floor_db, 0);
    if (!ic_nnr) return 0;
    ic_model = setModel_nnr(ic_nnr, model);
    ic_nnr_strength = strength;
    return 1;
}

__declspec(dllexport) int ic_wdsp_audio_open(void) {
    if (ic_buffer) return 1;
    ic_buffer = (double *)_aligned_malloc(IC_BLOCK_SIZE * sizeof(complex), 64);
    if (!ic_buffer) return 0;
    memset(ic_buffer, 0, IC_BLOCK_SIZE * sizeof(complex));

    ic_nnr = create_nnr(1, 0, IC_BLOCK_SIZE, ic_buffer, ic_buffer,
                        48000, -25.0, 0);
    ic_nr2 = create_emnr(1, 0, IC_BLOCK_SIZE, ic_buffer, ic_buffer,
                         4096, 4, 48000, 0, 1.0, 2, 0, 1);
    ic_anf = create_anf(1, 0, IC_BLOCK_SIZE, ic_buffer, ic_buffer,
                        ANF_DLINE_SIZE, 64, 16, 0.0001, 0.1,
                        1.0, 0.0, 200.0, 6.25e-12, 6.25e-10, 1.0, 3.0);
    ic_blanker = create_snba(1, ic_buffer, ic_buffer, 48000, 12000,
                              IC_BLOCK_SIZE, 4, 256, 64, 2, 8.0, 20.0,
                              10, 2, 2, 0.5, 200.0, 5400.0);
    if (!ic_nnr || !ic_nr2 || !ic_anf || !ic_blanker) {
        return 0;
    }
    ic_model = 0;
    ic_nnr_strength = 59;
    return 1;
}

__declspec(dllexport) void ic_wdsp_audio_reset(void) {
    if (ic_nnr) flush_nnr(ic_nnr);
    if (ic_nr2) flush_emnr(ic_nr2);
    if (ic_anf) flush_anf(ic_anf);
    if (ic_blanker) flush_snba(ic_blanker);
    if (ic_buffer) memset(ic_buffer, 0, IC_BLOCK_SIZE * sizeof(complex));
}

__declspec(dllexport) void ic_wdsp_audio_close(void) {
    if (ic_blanker) destroy_snba(ic_blanker);
    if (ic_anf) destroy_anf(ic_anf);
    if (ic_nr2) destroy_emnr(ic_nr2);
    if (ic_nnr) destroy_nnr(ic_nnr);
    if (ic_buffer) _aligned_free(ic_buffer);
    ic_blanker = 0;
    ic_anf = 0;
    ic_nr2 = 0;
    ic_nnr = 0;
    ic_buffer = 0;
}

__declspec(dllexport) int ic_wdsp_audio_process(
    float *samples, int frames, int algorithm, int model,
    int auto_notch, int noise_blanker, int reduction, int protection) {
    if (!samples || frames != IC_BLOCK_SIZE || !ic_buffer) return 0;
    if (model != ic_model) {
        ic_model = setModel_nnr(ic_nnr, model);
    }
    double strength = ic_effective_strength(reduction, protection);
    int strength_percent = (int)(strength * 100.0 + 0.5);
    if (algorithm != 0 && strength_percent != ic_nnr_strength) {
        if (!ic_recreate_nnr(strength_percent, model)) return 0;
    }
    for (int i = 0; i < frames; i++) {
        ic_buffer[2 * i + 0] = samples[i];
        ic_buffer[2 * i + 1] = 0.0;
    }
    if (noise_blanker) xsnba(ic_blanker);
    if (auto_notch) xanf(ic_anf, 0);
    if (strength > 0.0) {
        if (algorithm == 0) {
            /* Tune NR2's estimator instead of mixing delayed wet audio with
             * live dry audio. Higher q and lower xi_min increase suppression. */
            ic_nr2->g.q = 0.05 + 0.75 * strength;
            ic_nr2->g.xi_min = pow(10.0, (-5.0 - 35.0 * strength) / 10.0);
            xemnr(ic_nr2, 0);
        } else {
            xnnr(ic_nnr, 0);
        }
    }

    /* WDSP is intentionally 100% wet. NR2/NNR are frame-based and delayed;
     * adding the current dry sample here creates an audible second voice. */
    for (int i = 0; i < frames; i++) {
        samples[i] = (float)ic_buffer[2 * i];
    }
    return 1;
}
