#ifndef __AREA_TRIGGER_API__
#define __AREA_TRIGGER_API__

#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    int errorCode;
    bool found;
    uint32_t destinationMapID;
} GetAreaTriggerTeleportDestinationResponse;

typedef GetAreaTriggerTeleportDestinationResponse (*GetAreaTriggerTeleportDestinationHandler)(uint32_t triggerID);

#ifdef __cplusplus
}
#endif

#endif
