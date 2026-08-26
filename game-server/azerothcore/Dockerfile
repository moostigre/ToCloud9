FROM ubuntu:22.04 AS build-sidecar

ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /build

# Install build dependencies for libsidecar-cpp
RUN apt-get update && \
    apt-get install -y \
        git \
        cmake \
        make \
        gcc \
        g++ \
        build-essential \
        libssl-dev \
        zlib1g-dev \
        ca-certificates \
        pkg-config && \
    rm -rf /var/lib/apt/lists/*

# Copy source files (proto must be at /api/proto for CMake path resolution)
COPY api/proto /api/proto
COPY game-server/libsidecar-cpp /build/libsidecar-cpp

# Build libsidecar-cpp
WORKDIR /build/libsidecar-cpp
RUN mkdir -p build && cd build && \
    cmake .. \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_TESTS=OFF \
        -DUSE_SYSTEM_GRPC=OFF \
        -DUSE_SYSTEM_PROTOBUF=OFF && \
    cmake --build . --config Release -j $(nproc)

FROM ubuntu:22.04 AS build-core

ENV DEBIAN_FRONTEND=noninteractive

# runs from project directory
WORKDIR /

RUN apt-get update || true && \
    apt-get install -y --reinstall ubuntu-keyring && \
    apt-get update

RUN apt-get install -yq git clang cmake make gcc g++ libmysqlclient-dev libssl-dev libbz2-dev libreadline-dev libncurses-dev libboost-all-dev mysql-server p7zip
RUN update-alternatives --install /usr/bin/cc cc /usr/bin/clang 100
RUN update-alternatives --install /usr/bin/c++ c++ /usr/bin/clang 100

ARG AC_REPOSITORY=https://github.com/azerothcore/azerothcore-wotlk.git
ARG AC_REF=ca8e6d78dee182fbe4c840dbc24a577c25844593
ARG AC_BUILD_TYPE=RelWithDebInfo
ARG AC_MODULES=

RUN mkdir repo
RUN cd repo && git init && \
    case "$AC_REPOSITORY" in \
      https://github.com/azerothcore/azerothcore-wotlk.git|https://github.com/moostigre/azerothcore-wotlk.git) ;; \
      *) exit 1 ;; \
    esac && \
    git remote add origin "$AC_REPOSITORY" && \
    git fetch --depth 1 origin "$AC_REF" && \
    git checkout FETCH_HEAD

# Core repositories are explicitly allowlisted above. Every optional module is
# still restricted to the trusted AzerothCore organization and pinned to an
# exact commit resolved by CI.
RUN if [ -n "$AC_MODULES" ]; then \
      old_ifs="$IFS"; IFS=','; \
      for entry in $AC_MODULES; do \
        module="${entry%@*}"; module_ref="${entry#*@}"; \
        printf '%s' "$module" | grep -Eq '^[a-z0-9][a-z0-9-]{1,62}$'; \
        printf '%s' "$module_ref" | grep -Eq '^[a-f0-9]{40}$'; \
        mkdir -p "/repo/modules/$module"; \
        git -C "/repo/modules/$module" init; \
        git -C "/repo/modules/$module" remote add origin "https://github.com/azerothcore/$module.git"; \
        git -C "/repo/modules/$module" fetch --depth 1 origin "$module_ref"; \
        git -C "/repo/modules/$module" checkout --detach FETCH_HEAD; \
      done; \
      IFS="$old_ifs"; \
    fi

# Infrastructure-only command module used to create deterministic test
# characters through AzerothCore's own Player, quest, item and GUID APIs.
COPY game-server/azerothcore/testrealm-module /repo/modules/mod-testrealm-fixtures

# dbimport only carries /repo/data into its runtime image. Flatten the SQL from
# each exact, trusted module revision into the matching custom database tree so
# every ephemeral realm receives the module schema and strings it was compiled
# against. The collector ignores symlinks and caps the copied SQL volume.
COPY game-server/azerothcore/collect-module-sql.sh /usr/local/bin/collect-module-sql
RUN chmod 0555 /usr/local/bin/collect-module-sql && /usr/local/bin/collect-module-sql

COPY game-server/azerothcore/patches /tmp/azerothcore-patches
RUN cd /repo && \
    for patch in /tmp/azerothcore-patches/*.patch; do \
        [ -e "$patch" ] || continue; \
        git apply --check "$patch" && git apply "$patch"; \
    done

COPY --from=build-sidecar /build/libsidecar-cpp/build/libsidecar.so ./repo/deps/libsidecar/libsidecar.so
COPY --from=build-sidecar /build/libsidecar-cpp/include/libsidecar.h ./repo/deps/libsidecar/include/libsidecar.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/events-guild.h ./repo/deps/libsidecar/include/events-guild.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/events-group.h ./repo/deps/libsidecar/include/events-group.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/events-servers-registry.h ./repo/deps/libsidecar/include/events-servers-registry.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/player-items-api.h ./repo/deps/libsidecar/include/player-items-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/player-money-api.h ./repo/deps/libsidecar/include/player-money-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/player-interactions-api.h ./repo/deps/libsidecar/include/player-interactions-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/battleground-api.h ./repo/deps/libsidecar/include/battleground-api.h
COPY --from=build-sidecar /build/libsidecar-cpp/include/monitoring.h ./repo/deps/libsidecar/include/monitoring.h

WORKDIR /repo

RUN cd bin && \
    cmake .. -DUSE_REAL_LIBSIDECAR=ON \
        -DBoost_USE_STATIC_LIBS=ON \
        -DWITH_WARNINGS=1 \
        -DWITH_COREDEBUG=0 \
        -DUSE_COREPCH=1 \
        -DUSE_SCRIPTPCH=1 \
        -DTOOLS_BUILD=db-only \
        -DSCRIPTS=static \
        -DSERVERS=1 \
        -DCMAKE_BUILD_TYPE="${AC_BUILD_TYPE}" \
        -DCMAKE_INSTALL_PREFIX=/repo/bin \
        -DBUILD_TESTING=0

RUN cp deps/libsidecar/libsidecar.so /usr/lib/libsidecar.so

WORKDIR /repo/bin

RUN cmake --build . --config "${AC_BUILD_TYPE}" -j $(($(nproc) + 1))
RUN cmake --install . --config "${AC_BUILD_TYPE}"

FROM ubuntu:22.04 AS db-import

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      libmysqlclient21 libreadline8 \
      gettext-base default-mysql-client && \
    rm -rf /var/lib/apt/lists/*


COPY --from=build-sidecar /build/libsidecar-cpp/build/libsidecar.so* /usr/lib/

# The paths are hardcoded in the binary
COPY --from=build-core /repo/data /repo/data
COPY --from=build-core /repo/bin/bin/dbimport /repo/bin/dbimport
COPY --from=build-core /repo/bin/etc/dbimport.conf.dist /repo/bin/etc/dbimport.conf

COPY sql/auth/mysql/ /repo/data/sql/custom/db_auth
COPY sql/characters/mysql/*.up.sql /repo/data/sql/custom/db_characters

WORKDIR /repo/bin

ENTRYPOINT ["./dbimport"]

FROM ubuntu:22.04 AS gameserver

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      libmysqlclient21 libreadline8 \
      gettext-base default-mysql-client && \
    rm -rf /var/lib/apt/lists/*

COPY --from=build-sidecar /build/libsidecar-cpp/build/libsidecar.so* /usr/lib/

COPY --from=build-core /repo/bin/etc/worldserver.conf.dist /repo/bin/etc/worldserver.conf
COPY --from=build-core /repo/bin/bin/worldserver /repo/bin/worldserver

WORKDIR /repo/bin

ENTRYPOINT ["./worldserver"]
